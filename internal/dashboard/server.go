// Package dashboard provides a unified Web UI that aggregates multiple Belayer
// daemons. It serves the embedded static assets and reverse-proxies API calls
// to configured daemon backends.
package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/donovan-yohan/belayer"
	"go.yaml.in/yaml/v3"
)

// DaemonConfig describes one daemon backend the dashboard proxies to.
type DaemonConfig struct {
	Name  string `yaml:"name"`
	URL   string `yaml:"url"`
	Token string `yaml:"token"`
}

// ConfigFile is the YAML shape read from --config.
type ConfigFile struct {
	Daemons []DaemonConfig `yaml:"daemons"`
}

// Server is the dashboard HTTP server.
type Server struct {
	daemons []DaemonConfig
	proxies map[string]*httputil.ReverseProxy
}

// NewServer creates a dashboard server for the given daemon backends.
func NewServer(daemons []DaemonConfig) (*Server, error) {
	if len(daemons) == 0 {
		return nil, fmt.Errorf("dashboard: at least one daemon must be configured")
	}
	s := &Server{
		daemons: daemons,
		proxies: make(map[string]*httputil.ReverseProxy, len(daemons)),
	}
	for _, d := range daemons {
		target, err := url.Parse(d.URL)
		if err != nil {
			return nil, fmt.Errorf("dashboard: invalid URL for daemon %q: %w", d.Name, err)
		}
		proxy := httputil.NewSingleHostReverseProxy(target)
		proxy.FlushInterval = 100 * time.Millisecond
		originalDirector := proxy.Director
		prefix := "/api/daemons/" + d.Name
		proxy.Director = func(req *http.Request) {
			originalDirector(req)
			req.URL.Path = strings.TrimPrefix(req.URL.Path, prefix)
			req.Header.Set("Authorization", "Bearer "+d.Token)
		}
		s.proxies[d.Name] = proxy
	}
	return s, nil
}

// Handler returns the http.Handler for the dashboard.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ui/{$}", s.handleWebUI)
	mux.HandleFunc("GET /ui/{path...}", s.handleWebUI)
	mux.HandleFunc("GET /api/daemons", s.handleListDaemons)
	// Catch-all proxy for any method under /api/daemons/{name}/
	mux.HandleFunc("/api/daemons/{name}/", s.handleProxy)
	return mux
}

func (s *Server) handleWebUI(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/ui")
	if path == "" || path == "/" {
		path = "/index.html"
	}

	content, err := belayer.WebUI.ReadFile("web" + path)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	switch {
	case strings.HasSuffix(path, ".css"):
		w.Header().Set("Content-Type", "text/css")
	case strings.HasSuffix(path, ".js"):
		w.Header().Set("Content-Type", "application/javascript")
	default:
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	}
	w.Write(content)
}

type daemonInfo struct {
	Name    string `json:"name"`
	URL     string `json:"url"`
	Healthy bool   `json:"healthy"`
}

func (s *Server) handleListDaemons(w http.ResponseWriter, r *http.Request) {
	infos := make([]daemonInfo, len(s.daemons))
	var wg sync.WaitGroup
	for i, d := range s.daemons {
		wg.Add(1)
		go func(idx int, cfg DaemonConfig) {
			defer wg.Done()
			infos[idx] = daemonInfo{
				Name:    cfg.Name,
				URL:     cfg.URL,
				Healthy: s.checkHealth(cfg),
			}
		}(i, d)
	}
	wg.Wait()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(infos)
}

func (s *Server) checkHealth(d DaemonConfig) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	req, err := http.NewRequest("GET", d.URL+"/health", nil)
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+d.Token)
	res, err := client.Do(req)
	if err != nil {
		return false
	}
	defer res.Body.Close()
	return res.StatusCode == http.StatusOK
}

func (s *Server) handleProxy(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	proxy, ok := s.proxies[name]
	if !ok {
		http.NotFound(w, r)
		return
	}
	proxy.ServeHTTP(w, r)
}

// LoadConfig reads a dashboard YAML config file.
func LoadConfig(path string) ([]DaemonConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("dashboard: read config: %w", err)
	}
	var cfg ConfigFile
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("dashboard: parse config: %w", err)
	}
	return cfg.Daemons, nil
}
