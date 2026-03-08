package handler

import (
	"log/slog"
	"net/http"
	"slices"

	"github.com/cbellee/photo-api/internal/utils"
	"go.opentelemetry.io/otel/attribute"
)

// RequireRole is HTTP middleware that verifies a JWT bearer token and checks
// that the caller has the specified role claim. On failure it returns 401/403.
func RequireRole(cfg *Config, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, span := tracer.Start(r.Context(), "middleware.RequireRole")
		defer span.End()
		span.SetAttributes(attribute.String("auth.required_role", cfg.RoleName))

		claims, err := utils.VerifyToken(r, cfg.JwksURL, cfg.JWTKeyfunc)
		if err != nil {
			slog.ErrorContext(ctx, "token verification failed", "error", err)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		if !slices.Contains(claims.Roles, cfg.RoleName) {
			slog.WarnContext(ctx, "caller does not have required role", "required", cfg.RoleName, "roles", claims.Roles)
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		slog.DebugContext(ctx, "role claim found in token", "roles", claims.Roles)
		next(w, r)
	}
}
