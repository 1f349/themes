package main

import (
	_ "embed"
	"encoding/json"
	"flag"
	"html/template"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/log"
	"github.com/radovskyb/watcher"
)

var (
	Logger = log.NewWithOptions(os.Stderr, log.Options{
		ReportTimestamp: true,
		ReportCaller:    true,
	})
	Projects = []string{
		"lavender",
	}
	DefaultFuncMap = template.FuncMap{
		"emailHide": EmailHide,
	}
	ProjectPaths = make(map[string]bool)
	TemplateMu   = &sync.RWMutex{}
	TemplateMap  = make(map[string]*template.Template)
	TemplateData = make(map[string]any)

	//go:embed main-index.go.html
	MainIndexRaw string

	MainIndexTemplate = template.Must(template.New("main-index").Parse(MainIndexRaw))

	WatchMode bool
	DebugMode bool
)

func main() {
	flag.BoolVar(&WatchMode, "watch", false, "watch mode")
	flag.BoolVar(&DebugMode, "debug", false, "debug mode")
	flag.Parse()

	if DebugMode {
		Logger.SetLevel(log.DebugLevel)
	}

	Logger.Info("Starting theme development")

	BaseDir, err := os.Getwd()
	if err != nil {
		Logger.Fatal("Failed to get base directory", "err", err)
	}

	w := watcher.New()
	for _, i := range Projects {
		absPath, err := filepath.Abs(i)
		if err != nil {
			Logger.Warn("Failed to add project to watcher", "project", i, "err", err)
			continue
		}
		err = w.Add(absPath)
		if err != nil {
			Logger.Warn("Failed to add project to watcher", "project", i, "err", err)
			continue
		}
		ProjectPaths[absPath] = true
		UpdateTemplate(absPath)
	}

	if !WatchMode {
		Logger.Info("Build finished")
		return
	}
	Logger.Info("Starting watch mode")

	go UpdateOnChange(w.Event)
	go func() {
		err := w.Start(5 * time.Second)
		Logger.Warn("Watcher stopped", "err", err)
	}()

	_ = http.ListenAndServe(":8080", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			TemplateMu.RLock()
			defer TemplateMu.RUnlock()

			type ProjectData struct {
				Name  string
				Files []string
			}

			projects := make([]ProjectData, 0)

			for k, i := range TemplateMap {
				files := make([]string, 0, len(i.Templates()))
				for _, v2 := range i.Templates() {
					if v2.Name() != "theme-template-root" {
						files = append(files, v2.Name())
					}
				}
				sort.Strings(files)
				projects = append(projects, ProjectData{
					Name:  filepath.Base(k),
					Files: files,
				})
			}

			err := MainIndexTemplate.Execute(w, projects)
			if err != nil {
				Logger.Warn("Failed to render main-index.go.html", "err", err)
			}
			return
		}

		if r.URL.Path == "/favicon.ico" {
			http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
			return
		}

		if r.URL.Path == "/style.css" {
			parse, err := url.Parse(r.Referer())
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			project, _, ok := ParseProjectPath(parse.Path)
			if !ok {
				Logger.Warn("Invalid referrer path", "path", parse.Path)
				http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
				return
			}
			http.ServeFile(w, r, filepath.Join(BaseDir, project, "style.css"))
			return
		}

		if strings.Contains(r.URL.Path, "..") || strings.Contains(r.URL.RawPath, "..") {
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}

		project, path, ok := ParseProjectPath(r.URL.Path)
		if !ok {
			Logger.Warn("Invalid path", "path", r.URL.Path)
			http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
			return
		}
		p2 := filepath.Join(BaseDir, project)

		TemplateMu.RLock()
		defer TemplateMu.RUnlock()
		if TemplateMap[p2] == nil {
			Logger.Warn("Invalid project", "path", p2)
			http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
			return
		}
		err = TemplateMap[p2].ExecuteTemplate(w, path, TemplateData[p2])
		if err != nil {
			Logger.Warn("Failed to render template", "path", p2, "name", path, "err", err)
		}
	}))
}

func ParseProjectPath(path string) (string, string, bool) {
	p := path
	if strings.HasPrefix(p, "/") {
		p = p[1:]
	}
	n := strings.IndexByte(p, '/')
	if n == -1 {
		return "", "", false
	}
	return p[:n], p[n+1:], true
}

func UpdateOnChange(event <-chan watcher.Event) {
	for i := range event {
		Logger.Debug("Event", "op", i.Op, "name", i.FileInfo.Name(), "oldpath", i.OldPath, "path", i.Path)
		if ProjectPaths[i.Path] {
			UpdateTemplate(i.Path)
		}
		if ProjectPaths[i.OldPath] {
			UpdateTemplate(i.OldPath)
		}
	}
}

func UpdateTemplate(p string) {
	Logger.Info("Recompiling template", "path", p)

	// run tailwind command
	args := []string{"-i", filepath.Join(p, "main.css"), "-o", filepath.Join(p, "style.css"), "-c", filepath.Join(filepath.Dir(p), "tailwind.config.js"), "--minify"}
	cmd := exec.Command("tailwindcss", args...)
	cmd.Dir = p
	tailwindOutput, err := cmd.CombinedOutput()
	if err != nil {
		Logger.Warn("Failed to run tailwind", "err", err)
		return
	}
	Logger.Debug(string(tailwindOutput))

	// make new template
	fs, err := template.New("theme-template-root").Funcs(DefaultFuncMap).ParseFS(os.DirFS(p), "*.go.html")
	if err != nil {
		Logger.Warn("Failed to parse template", "err", err)
		return
	}
	dataJson, err := os.Open(filepath.Join(p, "data.json"))
	if err != nil {
		Logger.Warn("Failed to open data.json", "err", err)
		return
	}
	var dataBlob any
	err = json.NewDecoder(dataJson).Decode(&dataBlob)
	if err != nil {
		Logger.Warn("Failed to parse data.json", "err", err)
		return
	}
	TemplateMu.Lock()
	TemplateMap[p] = fs
	TemplateData[p] = dataBlob
	TemplateMu.Unlock()
}

func EmailHide(a string) string {
	b := []byte(a)
	for i := range b {
		if b[i] != '@' && b[i] != '.' {
			b[i] = 'x'
		}
	}
	return string(b)
}
