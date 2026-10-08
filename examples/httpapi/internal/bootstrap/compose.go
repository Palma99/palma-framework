//go:build pfw_inject

package bootstrap

import (
	"context"
	"net/http"

	pfw "github.com/palma99/palma-framework"
	"github.com/palma99/palma-framework/examples/httpapi/internal/config"
	apihttp "github.com/palma99/palma-framework/examples/httpapi/internal/platform/http"
	"github.com/palma99/palma-framework/examples/httpapi/internal/user/adapter/memory"
	"github.com/palma99/palma-framework/examples/httpapi/internal/user/adapter/postgres"
	"github.com/palma99/palma-framework/examples/httpapi/internal/user/application"
)

var Users = pfw.Module(
	pfw.Discover("../user/..."),
	pfw.Implementation[application.Repository, *postgres.Store](),
	pfw.ForEnv("local", pfw.Bind[application.Repository, *memory.Store]()),
	pfw.AutoBind(),
)

func Initialize(ctx context.Context, env pfw.Environment, cfg config.Config) (*http.Server, func() error, error) {
	return pfw.BuildWithCleanup[*http.Server](
		pfw.Environments(pfw.Local, pfw.Staging, "uat", pfw.Production),
		Users,
		pfw.AutoBind(),
		pfw.Constructors(apihttp.Routes, apihttp.Server),
	)
}
