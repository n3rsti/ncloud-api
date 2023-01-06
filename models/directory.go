package models

type Directory struct {
	id          string      `json:"id"`
	name        string      `json:"name"`
	files       []File      `json:"files"`
	directories []Directory `json:"directories"`
}
