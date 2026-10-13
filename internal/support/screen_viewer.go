		_, _ = w.Write(f.PNG)
	default:
		if path == "" {
			path = "index.html"
		}
		if path != "index.html" && path != "app.js" && path != "app.css" {
			http.NotFound(w, r)
			return
		}
		sub, _ := fs.Sub(viewerFiles, "viewer")
		copy := r.Clone(r.Context())
		copy.URL.Path = "/" + path
		http.FileServerFS(sub).ServeHTTP(w, copy)
	}
}
