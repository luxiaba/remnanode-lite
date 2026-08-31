package nodehandler

import "github.com/luxiaba/remnanode-lite/internal/nodeapi"

var (
	errInternalServer = nodeapi.ServiceError{Code: "A001", Message: "Server error", Status: 500}
)
