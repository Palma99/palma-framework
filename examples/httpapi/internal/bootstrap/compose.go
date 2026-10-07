//go:build pfw_inject

package bootstrap

import (
	"net/http"

	pfw "github.com/palma99/palma-framework"
	"github.com/palma99/palma-framework/examples/httpapi/internal/config"
	apihttp "github.com/palma99/palma-framework/examples/httpapi/internal/platform/http"
)

var Users = pfw.Module(
	pfw.Discover("../user/..."),
	pfw.AutoBind(),
)

func Initialize(cfg config.Config) (*http.Server, error) {
	return pfw.Build[*http.Server](Users, pfw.Constructors(apihttp.Routes, apihttp.Server))
}
