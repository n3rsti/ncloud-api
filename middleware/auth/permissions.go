package auth

const (
	PermissionRead   = "read"
	PermissionModify = "modify"
	PermissionDelete = "delete"
	PermissionUpload = "upload" // directory-only permission (uploading files access)

)

var AllDirectoryPermissions = []string{
	PermissionRead,
	PermissionModify,
	PermissionDelete,
	PermissionUpload,
}
