package gateway

import (
	"net/http"

	"gopkg.in/yaml.v3"
)

// handleOpenAPI serves the auto-generated OpenAPI 3.0 spec as YAML.
func (g *Gateway) handleOpenAPI() http.HandlerFunc {
	spec := buildOpenAPISpec()

	// Pre-marshal once; the spec is static for the lifetime of the process.
	data, err := yaml.Marshal(spec)
	if err != nil {
		// Should never happen with a well-formed map literal.
		panic("openapi: failed to marshal spec: " + err.Error())
	}

	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-yaml")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	}
}

// buildOpenAPISpec constructs the full OpenAPI 3.0 specification from code.
// Keeping it in Go means it stays in sync with the actual routes.
func buildOpenAPISpec() map[string]any {
	return map[string]any{
		"openapi": "3.0.3",
		"info": map[string]any{
			"title":       "Sclaw Gateway API",
			"description": "Administration, monitoring, and webhook endpoints for the Sclaw agent runtime.",
			"version":     "1.0.0",
		},
		"servers": []map[string]any{
			{"url": "http://127.0.0.1:8080", "description": "Local gateway (default)"},
		},
		"paths": mergePaths(
			healthPaths(),
			statusPaths(),
			sessionPaths(),
			agentPaths(),
			modulePaths(),
			configPaths(),
			cronPaths(),
			openapiPaths(),
		),
		"components": map[string]any{
			"securitySchemes": map[string]any{
				"bearerAuth": map[string]any{
					"type":   "http",
					"scheme": "bearer",
				},
				"basicAuth": map[string]any{
					"type":   "http",
					"scheme": "basic",
				},
			},
			"schemas": componentSchemas(),
		},
		"security": []map[string]any{
			{"bearerAuth": []string{}},
			{"basicAuth": []string{}},
		},
	}
}

func healthPaths() map[string]any {
	return map[string]any{
		"/health": map[string]any{
			"get": map[string]any{
				"summary":     "Health check",
				"operationId": "getHealth",
				"tags":        []string{"monitoring"},
				"security":    []map[string]any{},
				"responses": map[string]any{
					"200": map[string]any{
						"description": "Service is healthy",
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{"$ref": "#/components/schemas/HealthResponse"},
							},
						},
					},
				},
			},
		},
	}
}

func statusPaths() map[string]any {
	return map[string]any{
		"/status": map[string]any{
			"get": map[string]any{
				"summary":     "Runtime status with uptime, metrics, sessions, and provider health",
				"operationId": "getStatus",
				"tags":        []string{"monitoring"},
				"responses": map[string]any{
					"200": map[string]any{
						"description": "Current runtime status",
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{"$ref": "#/components/schemas/StatusResponse"},
							},
						},
					},
				},
			},
		},
	}
}

func sessionPaths() map[string]any {
	return map[string]any{
		"/api/sessions": map[string]any{
			"get": map[string]any{
				"summary":     "List all active sessions",
				"operationId": "listSessions",
				"tags":        []string{"sessions"},
				"responses": map[string]any{
					"200": map[string]any{
						"description": "Array of active sessions",
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{
									"type":  "array",
									"items": map[string]any{"$ref": "#/components/schemas/Session"},
								},
							},
						},
					},
				},
			},
		},
		"/api/sessions/{id}": map[string]any{
			"delete": map[string]any{
				"summary":     "Delete a session by ID",
				"operationId": "deleteSession",
				"tags":        []string{"sessions"},
				"parameters": []map[string]any{
					{"name": "id", "in": "path", "required": true, "schema": map[string]any{"type": "string"}},
				},
				"responses": map[string]any{
					"204": map[string]any{"description": "Session deleted"},
					"404": map[string]any{"description": "Session not found"},
				},
			},
		},
	}
}

func agentPaths() map[string]any {
	return map[string]any{
		"/api/agents": map[string]any{
			"get": map[string]any{
				"summary":     "List registered agents",
				"operationId": "listAgents",
				"tags":        []string{"agents"},
				"responses": map[string]any{
					"200": map[string]any{
						"description": "Array of agents",
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{
									"type":  "array",
									"items": map[string]any{"$ref": "#/components/schemas/Agent"},
								},
							},
						},
					},
				},
			},
		},
	}
}

func modulePaths() map[string]any {
	return map[string]any{
		"/api/modules": map[string]any{
			"get": map[string]any{
				"summary":     "List all compiled modules",
				"operationId": "listModules",
				"tags":        []string{"modules"},
				"responses": map[string]any{
					"200": map[string]any{
						"description": "Array of modules",
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{
									"type":  "array",
									"items": map[string]any{"$ref": "#/components/schemas/Module"},
								},
							},
						},
					},
				},
			},
		},
	}
}

func configPaths() map[string]any {
	return map[string]any{
		"/api/config": map[string]any{
			"get": map[string]any{
				"summary":     "Get current configuration (secrets redacted)",
				"operationId": "getConfig",
				"tags":        []string{"config"},
				"responses": map[string]any{
					"200": map[string]any{
						"description": "Redacted configuration object",
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{"type": "object"},
							},
						},
					},
				},
			},
		},
		"/api/config/reload": map[string]any{
			"post": map[string]any{
				"summary":     "Trigger a hot-reload of the configuration",
				"operationId": "reloadConfig",
				"tags":        []string{"config"},
				"responses": map[string]any{
					"200": map[string]any{
						"description": "Configuration reloaded successfully",
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{
									"type": "object",
									"properties": map[string]any{
										"status": map[string]any{"type": "string"},
									},
								},
							},
						},
					},
					"400": map[string]any{"description": "Invalid configuration"},
				},
			},
		},
	}
}

func cronPaths() map[string]any {
	return map[string]any{
		"/api/crons": map[string]any{
			"get": map[string]any{
				"summary":     "List all registered prompt crons",
				"operationId": "listCrons",
				"tags":        []string{"crons"},
				"responses": map[string]any{
					"200": map[string]any{
						"description": "Array of prompt cron definitions with last results",
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{
									"type":  "array",
									"items": map[string]any{"$ref": "#/components/schemas/CronInfo"},
								},
							},
						},
					},
				},
			},
		},
		"/api/crons/{name}": map[string]any{
			"get": map[string]any{
				"summary":     "Get a prompt cron definition and its last result",
				"operationId": "getCron",
				"tags":        []string{"crons"},
				"parameters": []map[string]any{
					{"name": "name", "in": "path", "required": true, "schema": map[string]any{"type": "string"}, "description": "Cron definition name"},
				},
				"responses": map[string]any{
					"200": map[string]any{
						"description": "Cron definition with last result",
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{"$ref": "#/components/schemas/CronInfo"},
							},
						},
					},
					"404": map[string]any{"description": "Cron not found"},
				},
			},
		},
		"/api/crons/{name}/trigger": map[string]any{
			"post": map[string]any{
				"summary":     "Manually trigger a prompt cron (runs in background)",
				"operationId": "triggerCron",
				"tags":        []string{"crons"},
				"parameters": []map[string]any{
					{"name": "name", "in": "path", "required": true, "schema": map[string]any{"type": "string"}, "description": "Cron definition name"},
				},
				"responses": map[string]any{
					"202": map[string]any{
						"description": "Cron triggered successfully (running in background)",
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{"$ref": "#/components/schemas/TriggerResponse"},
							},
						},
					},
					"404": map[string]any{"description": "Cron not found"},
					"503": map[string]any{"description": "Cron trigger service not available"},
				},
			},
		},
	}
}

func openapiPaths() map[string]any {
	return map[string]any{
		"/api/openapi.yaml": map[string]any{
			"get": map[string]any{
				"summary":     "OpenAPI 3.0 specification",
				"operationId": "getOpenAPISpec",
				"tags":        []string{"meta"},
				"responses": map[string]any{
					"200": map[string]any{
						"description": "OpenAPI YAML specification",
						"content": map[string]any{
							"application/x-yaml": map[string]any{
								"schema": map[string]any{"type": "string"},
							},
						},
					},
				},
			},
		},
	}
}

func componentSchemas() map[string]any {
	return map[string]any{
		"HealthResponse": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"status": map[string]any{"type": "string", "example": "ok"},
			},
		},
		"StatusResponse": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"uptime_seconds": map[string]any{"type": "integer", "format": "int64"},
				"metrics":        map[string]any{"type": "object"},
				"sessions":       map[string]any{"type": "integer"},
				"providers":      map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
			},
		},
		"Session": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":             map[string]any{"type": "string"},
				"channel":        map[string]any{"type": "string"},
				"chat_id":        map[string]any{"type": "string"},
				"thread_id":      map[string]any{"type": "string"},
				"agent_id":       map[string]any{"type": "string"},
				"created_at":     map[string]any{"type": "string", "format": "date-time"},
				"last_active_at": map[string]any{"type": "string", "format": "date-time"},
				"history_len":    map[string]any{"type": "integer"},
				"metadata":       map[string]any{"type": "object", "nullable": true},
			},
		},
		"Agent": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":        map[string]any{"type": "string"},
				"namespace": map[string]any{"type": "string"},
				"name":      map[string]any{"type": "string"},
			},
		},
		"Module": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":        map[string]any{"type": "string"},
				"namespace": map[string]any{"type": "string"},
				"name":      map[string]any{"type": "string"},
			},
		},
		"CronInfo": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name":        map[string]any{"type": "string"},
				"description": map[string]any{"type": "string"},
				"schedule":    map[string]any{"type": "string", "example": "0 7 * * *"},
				"enabled":     map[string]any{"type": "boolean"},
				"agent_id":    map[string]any{"type": "string"},
				"last_result": map[string]any{
					"nullable": true,
					"$ref":     "#/components/schemas/CronResult",
				},
			},
		},
		"CronResult": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name":         map[string]any{"type": "string"},
				"ran_at":       map[string]any{"type": "string", "format": "date-time"},
				"duration_ms":  map[string]any{"type": "integer", "format": "int64"},
				"stop_reason":  map[string]any{"type": "string"},
				"iterations":   map[string]any{"type": "integer"},
				"tool_calls":   map[string]any{"type": "integer"},
				"total_tokens": map[string]any{"type": "integer"},
				"content":      map[string]any{"type": "string"},
				"error":        map[string]any{"type": "string"},
			},
		},
		"TriggerResponse": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"status":   map[string]any{"type": "string", "example": "triggered"},
				"name":     map[string]any{"type": "string"},
				"agent_id": map[string]any{"type": "string"},
			},
		},
	}
}

// mergePaths combines multiple path maps into one.
func mergePaths(maps ...map[string]any) map[string]any {
	result := make(map[string]any)
	for _, m := range maps {
		for k, v := range m {
			result[k] = v
		}
	}
	return result
}
