package apiaction

type APIAction string

const (
	Create APIAction = "create"
	Update APIAction = "update"
	Delete APIAction = "delete"
	List   APIAction = "list"
	Get    APIAction = "get"
	FindBy APIAction = "findby"
)

func (a APIAction) String() string {
	return string(a)
}
