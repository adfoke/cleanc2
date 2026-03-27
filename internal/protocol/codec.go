package protocol

import "encoding/json"

func MarshalMessage(msgType string, payload any) ([]byte, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	return json.Marshal(Envelope{
		Type:    msgType,
		Payload: raw,
	})
}

func UnmarshalPayload[T any](env Envelope) (T, error) {
	var out T
	err := json.Unmarshal(env.Payload, &out)
	return out, err
}
