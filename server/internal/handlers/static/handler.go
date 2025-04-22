package static

import "net/http"

func NewHandler(staticAssetPath string) http.Handler {
	return http.FileServer(http.Dir(staticAssetPath))
}
