package server

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"cleanc2/internal/common"
	"cleanc2/internal/protocol"
)

type Service struct {
	cfg     Config
	logger  *zap.Logger
	store   *Store
	plugins *PluginManager
	engine  *gin.Engine
	httpSrv *http.Server

	mu         sync.RWMutex
	clients    map[string]*agentConn
	transferMu sync.RWMutex
	transfers  map[string]*transferState
}

type agentConn struct {
	id        string
	meta      protocol.AgentHello
	conn      *websocket.Conn
	send      chan outboundMessage
	service   *Service
	closeOnce sync.Once
}

type taskRequest struct {
	AgentID     string `json:"agent_id" binding:"required"`
	Command     string `json:"command" binding:"required"`
	TimeoutSecs int    `json:"timeout_secs"`
	Priority    int    `json:"priority"`
}

type batchTaskRequest struct {
	AgentIDs    []string `json:"agent_ids"`
	GroupIDs    []string `json:"group_ids"`
	Tags        []string `json:"tags"`
	Command     string   `json:"command" binding:"required"`
	TimeoutSecs int      `json:"timeout_secs"`
	Priority    int      `json:"priority"`
}

type groupRequest struct {
	ID          string   `json:"id"`
	Name        string   `json:"name" binding:"required"`
	Description string   `json:"description"`
	AgentIDs    []string `json:"agent_ids"`
}

type fileTransferRequest struct {
	AgentID    string `json:"agent_id" binding:"required"`
	LocalPath  string `json:"local_path" binding:"required"`
	RemotePath string `json:"remote_path" binding:"required"`
	ChunkSize  int    `json:"chunk_size"`
}

type taskDispatchResponse struct {
	TaskID     string `json:"task_id"`
	AgentID    string `json:"agent_id"`
	Dispatched bool   `json:"dispatched"`
	QueuedOnly bool   `json:"queued_only"`
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func New(cfg Config, logger *zap.Logger) (*Service, error) {
	if cfg.ListenAddr == "" {
		return nil, errors.New("listen addr is required")
	}
	if cfg.AuthToken == "" {
		return nil, errors.New("auth token is required")
	}
	if cfg.APIToken == "" {
		cfg.APIToken = cfg.AuthToken
	}
	if cfg.WriteWait == 0 {
		cfg.WriteWait = 10 * time.Second
	}
	if cfg.PongWait == 0 {
		cfg.PongWait = 70 * time.Second
	}
	if cfg.PingPeriod == 0 {
		cfg.PingPeriod = 25 * time.Second
	}

	svc := &Service{
		cfg:       cfg,
		logger:    logger,
		clients:   make(map[string]*agentConn),
		transfers: make(map[string]*transferState),
	}

	store, err := NewStore(cfg.DBPath)
	if err != nil {
		return nil, err
	}
	svc.store = store
	plugins, err := NewPluginManager(cfg.PluginDir, logger)
	if err != nil {
		return nil, err
	}
	svc.plugins = plugins
	svc.engine = svc.routes()
	svc.httpSrv = &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: svc.engine,
	}

	tlsCfg, err := buildServerTLSConfig(cfg)
	if err != nil {
		return nil, err
	}
	if tlsCfg != nil {
		svc.httpSrv.TLSConfig = tlsCfg
	}

	return svc, nil
}

func (s *Service) Run() error {
	s.logger.Info("server listening", zap.String("addr", s.cfg.ListenAddr))
	if s.httpSrv.TLSConfig != nil {
		return s.httpSrv.ListenAndServeTLS(s.cfg.TLSCertFile, s.cfg.TLSKeyFile)
	}
	return s.httpSrv.ListenAndServe()
}

func (s *Service) Shutdown(ctx context.Context) error {
	defer s.store.Close()
	return s.httpSrv.Shutdown(ctx)
}

func (s *Service) routes() *gin.Engine {
	engine := gin.New()
	engine.Use(gin.Recovery())
	engine.Use(s.requireOperatorAuth())

	engine.GET("/", s.handleDashboard)
	engine.GET("/dashboard", s.handleDashboard)

	engine.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	engine.GET("/api/v1/agents", func(c *gin.Context) {
		agents, err := s.store.Agents()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, agents)
	})

	engine.GET("/api/v1/agents/:id/metrics", func(c *gin.Context) {
		metrics, ok, err := s.store.AgentMetrics(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "metrics not found"})
			return
		}
		c.JSON(http.StatusOK, metrics)
	})

	engine.GET("/api/v1/metrics/overview", func(c *gin.Context) {
		agents, err := s.store.Agents()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		online := 0
		pending := 0
		for _, agent := range agents {
			if agent.Online {
				online++
			}
			pending += agent.PendingCount
		}
		c.JSON(http.StatusOK, gin.H{
			"total_agents":     len(agents),
			"online_agents":    online,
			"pending_tasks":    pending,
			"active_transfers": s.activeTransfersCount(),
			"plugins":          len(s.plugins.List()),
		})
	})

	engine.GET("/api/v1/plugins", func(c *gin.Context) {
		c.JSON(http.StatusOK, s.plugins.List())
	})

	engine.GET("/api/v1/groups", func(c *gin.Context) {
		groups, err := s.store.Groups()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, groups)
	})

	engine.GET("/api/v1/groups/:id", func(c *gin.Context) {
		group, ok, err := s.store.Group(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "group not found"})
			return
		}
		c.JSON(http.StatusOK, group)
	})

	engine.GET("/api/v1/tasks", func(c *gin.Context) {
		tasks, err := s.store.RecentTasks(queryLimit(c, 20, 100))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, tasks)
	})

	engine.POST("/api/v1/groups", func(c *gin.Context) {
		var req groupRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		group := Group{
			ID:          req.ID,
			Name:        req.Name,
			Description: req.Description,
			AgentIDs:    req.AgentIDs,
			CreatedAt:   time.Now().UTC(),
		}
		if group.ID == "" {
			group.ID = common.NewID()
		}
		if err := s.store.CreateOrUpdateGroup(group); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusAccepted, group)
	})

	engine.GET("/api/v1/tasks/:id", func(c *gin.Context) {
		task, ok, err := s.store.Task(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
			return
		}
		c.JSON(http.StatusOK, task)
	})

	engine.POST("/api/v1/tasks", func(c *gin.Context) {
		var req taskRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if req.TimeoutSecs <= 0 {
			req.TimeoutSecs = 60
		}

		resp, err := s.createTask(req.AgentID, req.Command, req.TimeoutSecs, req.Priority)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusAccepted, resp)
	})

	engine.POST("/api/v1/tasks/batch", func(c *gin.Context) {
		var req batchTaskRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if req.TimeoutSecs <= 0 {
			req.TimeoutSecs = 60
		}

		targets, err := s.resolveTargetAgentIDs(req.AgentIDs, req.GroupIDs, req.Tags)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		results := make([]taskDispatchResponse, 0, len(targets))
		for _, agentID := range targets {
			resp, err := s.createTask(agentID, req.Command, req.TimeoutSecs, req.Priority)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			results = append(results, resp)
		}

		c.JSON(http.StatusAccepted, gin.H{"count": len(results), "tasks": results})
	})

	engine.POST("/api/v1/tasks/:id/cancel", func(c *gin.Context) {
		task, state, ok, err := s.store.CancelTask(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
			return
		}

		cancelSent := false
		if state == "cancel_requested" {
			cancelSent = s.sendTaskCancel(task) == nil
		}

		c.JSON(http.StatusAccepted, gin.H{
			"task_id":     task.ID,
			"agent_id":    task.AgentID,
			"state":       state,
			"cancel_sent": cancelSent,
		})
	})

	engine.POST("/api/v1/files/upload", func(c *gin.Context) {
		var req fileTransferRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		transfer, err := s.startUpload(req.AgentID, req.LocalPath, req.RemotePath, req.ChunkSize)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusAccepted, transfer)
	})

	engine.POST("/api/v1/files/download", func(c *gin.Context) {
		var req fileTransferRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		transfer, err := s.startDownload(req.AgentID, req.RemotePath, req.LocalPath, req.ChunkSize)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusAccepted, transfer)
	})

	engine.GET("/api/v1/transfers/:id", func(c *gin.Context) {
		transfer, ok := s.transferSnapshot(c.Param("id"))
		if !ok {
			audit, ok, err := s.store.TransferAudit(c.Param("id"))
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			if !ok {
				c.JSON(http.StatusNotFound, gin.H{"error": "transfer not found"})
				return
			}
			c.JSON(http.StatusOK, audit)
			return
		}
		c.JSON(http.StatusOK, transfer)
	})

	engine.GET("/api/v1/transfers", func(c *gin.Context) {
		transfers, err := s.store.RecentTransferAudits(queryLimit(c, 20, 100))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, transfers)
	})

	engine.GET("/ws/agent", s.handleAgentWS)
	return engine
}

func (s *Service) handleAgentWS(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		s.logger.Warn("upgrade websocket", zap.Error(err))
		return
	}

	agent := &agentConn{
		conn:    conn,
		send:    make(chan outboundMessage, 16),
		service: s,
	}
	go agent.writeLoop()
	agent.readLoop()
}

func (s *Service) dispatchOrQueue(task protocol.Task) (bool, error) {
	if err := s.store.AddTask(task); err != nil {
		return false, err
	}

	s.mu.RLock()
	client, ok := s.clients[task.AgentID]
	s.mu.RUnlock()

	if ok {
		if err := client.sendTask(task); err == nil {
			return true, nil
		}
	}
	return false, nil
}

func (s *Service) register(client *agentConn, hello protocol.AgentHello) ([]protocol.Task, error) {
	client.id = hello.AgentID
	client.meta = hello

	s.mu.Lock()
	if old := s.clients[hello.AgentID]; old != nil && old != client {
		old.close()
	}
	s.clients[hello.AgentID] = client
	s.mu.Unlock()

	if err := s.store.UpsertAgent(AgentState{
		AgentID:     hello.AgentID,
		Hostname:    hello.Hostname,
		OS:          hello.OS,
		Arch:        hello.Arch,
		Tags:        hello.Tags,
		Fingerprint: hello.Fingerprint,
		Online:      true,
		LastSeenAt:  time.Now().UTC(),
		ConnectedAt: hello.ConnectedAt,
	}); err != nil {
		return nil, err
	}

	return s.store.PendingTasks(hello.AgentID)
}

func (s *Service) touch(agentID string) {
	if err := s.store.SetAgentOnline(agentID, true, time.Now().UTC()); err != nil {
		s.logger.Warn("touch agent", zap.String("agent_id", agentID), zap.Error(err))
	}
}

func (s *Service) unregister(client *agentConn) {
	if client.id == "" {
		client.close()
		return
	}

	s.mu.Lock()
	if current := s.clients[client.id]; current == client {
		delete(s.clients, client.id)
	}
	s.mu.Unlock()

	if err := s.store.SetAgentOnline(client.id, false, time.Now().UTC()); err != nil {
		s.logger.Warn("set agent offline", zap.String("agent_id", client.id), zap.Error(err))
	}
	client.close()
}

func (s *Service) createTask(agentID, command string, timeoutSecs, priority int) (taskDispatchResponse, error) {
	task := protocol.Task{
		ID:          common.NewID(),
		AgentID:     agentID,
		Type:        "shell",
		Command:     command,
		TimeoutSecs: timeoutSecs,
		Priority:    priority,
		CreatedAt:   time.Now().UTC(),
	}

	dispatched, err := s.dispatchOrQueue(task)
	if err != nil {
		return taskDispatchResponse{}, err
	}
	return taskDispatchResponse{
		TaskID:     task.ID,
		AgentID:    task.AgentID,
		Dispatched: dispatched,
		QueuedOnly: !dispatched,
	}, nil
}

func (s *Service) resolveTargetAgentIDs(agentIDs, groupIDs, tags []string) ([]string, error) {
	set := make(map[string]struct{})
	for _, agentID := range agentIDs {
		agentID = strings.TrimSpace(agentID)
		if agentID != "" {
			set[agentID] = struct{}{}
		}
	}

	if len(groupIDs) > 0 {
		groupAgentIDs, err := s.store.ResolveGroupAgentIDs(groupIDs)
		if err != nil {
			return nil, err
		}
		for _, agentID := range groupAgentIDs {
			set[agentID] = struct{}{}
		}
	}

	if len(tags) > 0 {
		agents, err := s.store.Agents()
		if err != nil {
			return nil, err
		}
		for _, agent := range agents {
			if matchAgentTags(agent.Tags, tags) {
				set[agent.AgentID] = struct{}{}
			}
		}
	}

	if len(set) == 0 {
		return nil, errors.New("no target agents")
	}

	targets := make([]string, 0, len(set))
	for agentID := range set {
		targets = append(targets, agentID)
	}
	sort.Strings(targets)
	return targets, nil
}

func (s *Service) sendTaskCancel(task protocol.Task) error {
	client, err := s.clientForAgent(task.AgentID)
	if err != nil {
		return err
	}
	return client.sendMessage(protocol.TypeTaskCancel, protocol.TaskCancel{
		TaskID:      task.ID,
		AgentID:     task.AgentID,
		RequestedAt: time.Now().UTC(),
	})
}

func (s *Service) requireOperatorAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		switch c.Request.URL.Path {
		case "/healthz", "/ws/agent":
			c.Next()
			return
		}
		if s.authorizedOperator(c.Request) {
			c.Next()
			return
		}
		c.Header("WWW-Authenticate", `Basic realm="cleanc2"`)
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
	}
}

func (s *Service) authorizedOperator(r *http.Request) bool {
	token := s.cfg.APIToken
	if token == "" {
		return false
	}
	if auth := strings.TrimSpace(r.Header.Get("Authorization")); auth != "" {
		if user, pass, ok := r.BasicAuth(); ok && user != "" {
			return subtle.ConstantTimeCompare([]byte(pass), []byte(token)) == 1
		}
		if bearer, ok := strings.CutPrefix(auth, "Bearer "); ok {
			return subtle.ConstantTimeCompare([]byte(strings.TrimSpace(bearer)), []byte(token)) == 1
		}
	}
	if headerToken := strings.TrimSpace(r.Header.Get("X-Auth-Token")); headerToken != "" {
		return subtle.ConstantTimeCompare([]byte(headerToken), []byte(token)) == 1
	}
	return false
}

func buildServerTLSConfig(cfg Config) (*tls.Config, error) {
	if cfg.TLSCertFile == "" || cfg.TLSKeyFile == "" {
		return nil, nil
	}

	tlsCfg := &tls.Config{
		MinVersion: tls.VersionTLS13,
	}
	if cfg.ClientCAFile == "" {
		return tlsCfg, nil
	}

	caPEM, err := os.ReadFile(cfg.ClientCAFile)
	if err != nil {
		return nil, fmt.Errorf("read client ca: %w", err)
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, errors.New("append client ca failed")
	}

	tlsCfg.ClientCAs = pool
	tlsCfg.ClientAuth = tls.RequireAndVerifyClientCert
	return tlsCfg, nil
}

func queryLimit(c *gin.Context, fallback, max int) int {
	limit, err := strconv.Atoi(c.DefaultQuery("limit", strconv.Itoa(fallback)))
	if err != nil || limit <= 0 {
		return fallback
	}
	if limit > max {
		return max
	}
	return limit
}
