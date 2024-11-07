package client

import "fmt"

type ClientType string

const (
	ClientREST ClientType = "REST"
	ClientHelm ClientType = "HELM"
)

func ToClientType(s string) (ClientType, error) {
	switch s {
	case "REST":
		return ClientREST, nil
	case "HELM":
		return ClientHelm, nil
	default:
		return "", fmt.Errorf("unknown client type %q", s)
	}
}

func (ct ClientType) String() string {
	return string(ct)
}
