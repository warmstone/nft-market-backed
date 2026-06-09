package handler

import (
	"net/http"

	gqlgen "nft-market-backend/internal/graphql"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/playground"
	"github.com/gin-gonic/gin"
)

type GraphQLHandler struct {
	srv *handler.Server
}

func NewGraphQLHandler(resolver *gqlgen.Resolver) *GraphQLHandler {
	srv := handler.NewDefaultServer(gqlgen.NewExecutableSchema(gqlgen.Config{Resolvers: resolver}))
	return &GraphQLHandler{srv: srv}
}

func (h *GraphQLHandler) Handle(c *gin.Context) {
	h.srv.ServeHTTP(c.Writer, c.Request)
}

func (h *GraphQLHandler) Playground(c *gin.Context) {
	playground.Handler("GraphQL", "/api/v1/graphql").ServeHTTP(c.Writer, c.Request)
}

func (h *GraphQLHandler) PlaygroundHandler() http.Handler {
	return playground.Handler("GraphQL", "/api/v1/graphql")
}
