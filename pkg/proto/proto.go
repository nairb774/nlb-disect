package proto

type Message struct {
	Name string `json:"name"`

	Close bool `json:"close,omitempty"`
}
