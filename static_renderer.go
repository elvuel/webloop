package webloop

import (
	"github.com/sqs/gotk3/gtk"
	"log"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"
)

// StaticRenderer generates and returns static HTML based on a snapshot of a Web
// page's computed HTML.
type StaticRenderer struct {
	// TargetBaseURL is the baseURL of the dynamic content URLs.
	TargetBaseURL string

	// Context is the WebLoop context to create views in.
	Context Context

	// WaitTimeout is the maximum duration to wait for a loaded page to set
	// window.$renderStaticReady.
	WaitTimeout time.Duration

	// Log is the logger to use for log messages. If nil, there is no log
	// output.
	Log *log.Logger

	viewLock sync.Mutex
	view     *View
}

var startGTKOnce sync.Once

// StartGTK ensures that the GTK+ main loop has started. If it has already been
// started by StartGTK, it will not start it again. If another goroutine is
// already running the GTK+ main loop, StartGTK's behavior is undefined.
func (h *StaticRenderer) StartGTK() {
	startGTKOnce.Do(func() {
		gtk.Init(nil)
		go func() {
			runtime.LockOSThread()
			gtk.Main()
		}()
	})
}

// Release releases resources used by this handler, such as the view. If this
// handler is reused after calling Release, the view is automatically recreated.
func (h *StaticRenderer) Release() {
	h.viewLock.Lock()
	h.view.Close()
	h.view = nil
	defer h.viewLock.Unlock()
}

// ServeHTTP implements net/http.Handler.
func (h *StaticRenderer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.StartGTK()
	h.viewLock.Lock()
	defer h.viewLock.Unlock()

	if h.view == nil {
		h.view = h.Context.NewView()
	}

	targetURL := h.TargetBaseURL + r.URL.String()
	h.logf("Rendering HTML for page at URL: %s", targetURL)
	h.view.Open(targetURL)
	h.view.Wait()

	// Wait until window.$renderStaticReady is true.
	start := time.Now()
	for {
		if time.Since(start) > h.WaitTimeout {
			h.logf("Page at URL %s did not set $renderStaticReady within timeout %s", targetURL, h.WaitTimeout)
			http.Error(w, "No response from origin server within "+h.WaitTimeout.String(), http.StatusBadGateway)
			return
		}

		ready, err := h.view.EvaluateJavaScript("window.$renderStaticReady")
		if err != nil {
			http.Error(w, "error checking $renderStaticReady: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if ready, ok := ready.(bool); !ok || !ready {
			time.Sleep(time.Millisecond * 50)
			continue
		}

		result, err := h.view.EvaluateJavaScript("document.documentElement.outerHTML")
		if err != nil {
			h.logf("Failed to dump HTML for page at URL %s: %s", targetURL, err)
			http.Error(w, "", http.StatusInternalServerError)
			return
		}

		html := result.(string)
		html = strings.Replace(html, "<body>", `<body><h3>This is a static page generated from <a href="`+r.URL.String()+`">`+r.URL.String()+`</a></h3><hr>`, 1)
		html = strings.Replace(html, "<script", `<script type="text/disabled"`, -1)
		w.Write([]byte(html))
		return
	}
}

func (h *StaticRenderer) logf(msg string, v ...interface{}) {
	if h.Log != nil {
		h.Log.Printf(msg, v...)
	}
}