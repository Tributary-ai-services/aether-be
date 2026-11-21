package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestInternalAuthRequired(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(InternalAuthRequired("s3cret-token"))
	r.GET("/x", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	for _, c := range []struct {
		name, header string
		want         int
	}{
		{"missing", "", http.StatusUnauthorized},
		{"wrong", "nope", http.StatusUnauthorized},
		{"prefix of the right token", "s3cret", http.StatusUnauthorized},
		{"right", "s3cret-token", http.StatusNoContent},
	} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		if c.header != "" {
			req.Header.Set("Internal-Auth", c.header)
		}
		r.ServeHTTP(w, req)
		if w.Code != c.want {
			t.Errorf("%s: %d, want %d", c.name, w.Code, c.want)
		}
	}
	defer func() {
		if recover() == nil {
			t.Error("an empty expected token must panic (fail closed), not mount open")
		}
	}()
	InternalAuthRequired("")
}
