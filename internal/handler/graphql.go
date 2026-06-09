package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// GraphQLHandler provides a stub GraphQL endpoint for v1.
// Full GraphQL schema and resolvers are deferred to post-v1; the REST API
// covers all core use cases (order CRUD, collection browse, stats).
type GraphQLHandler struct{}

// NewGraphQLHandler creates a GraphQLHandler.
func NewGraphQLHandler() *GraphQLHandler {
	return &GraphQLHandler{}
}

// Handle handles POST /api/v1/graphql.
func (h *GraphQLHandler) Handle(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{
		"error":   "NOT_IMPLEMENTED",
		"message": "GraphQL endpoint is deferred to post-v1. Use REST endpoints for all queries.",
	})
}
