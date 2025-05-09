package master

import (
	"api/core/master/admin"
	"api/core/master/api"
	"api/core/master/authentication"
	"api/core/master/dashboard"
	"api/core/master/landing"
	_ "api/core/models"
	"api/core/models/server"
	"net/http"
)

var (
	Service *server.Server = server.NewServer(&server.Config{
		Addr:   "10.0.0.111:8080",
	
	})
	Route  *server.Route   = server.NewSubRouter("")
	Assets *server.Handler = server.NewHandler("/_assets/", http.StripPrefix("/_assets", http.FileServer(http.Dir("assets/branding"))))
)

func NewV2() {
	Route.NewSubs(
		api.Route,
		authentication.Route,
		dashboard.Route,
		admin.Route,
		landing.Route,
	)
	Service.AddRoute(Route)
	Service.AddHandler(Assets)
	
	http.HandleFunc("/404", NotFoundHandler)
	Service.AddRoute(server.NewRoute("*", nil))
	if err := Service.ListenAndServe(); err != nil {
		panic(err)
	}
}