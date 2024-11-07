package apiaction

import (
	"fmt"
	"strings"
)

type APIAction string

const (
	Create APIAction = "create"
	Update APIAction = "update"
	Delete APIAction = "delete"
	List   APIAction = "list"
	Get    APIAction = "get"
	FindBy APIAction = "findby"
)

func StringToProviderRuntimeToAction(action string) (APIAction, error) {
	action = strings.ToLower(action)
	switch action {
	case "create":
		return Create, nil
	case "update":
		return Update, nil
	case "delete":
		return Delete, nil
	case "list":
		return List, nil
	case "findby":
		return FindBy, nil
	}
	return "", fmt.Errorf("invalid action %s", action)
}

func (a APIAction) String() string {
	return string(a)
}
