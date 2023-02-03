package models

type File struct {
	Id              string `json:"id"`
	Name            string `json:"name"`
	ParentDirectory string `json:"parent_directory,omitempty"`
	User            string `json:"user"`
	Type            string `json:"type"`
}
