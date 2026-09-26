package main

import (
	"context"
	"embed"
	"errors"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
	vc "vault/core/crypto"
	vd "vault/core/desktop"
	vs "vault/core/sync"
)

//go:embed all:frontend/dist
var assets embed.FS

type App struct {
	ctx          context.Context
	service      *vd.Service
	previewMu    sync.Mutex
	preview      vd.Preview
	previewToken string
}

func safe(e error) error {
	if e == nil {
		return nil
	}
	return errors.New(vd.Code(e))
}
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	runtime.OnFileDrop(ctx, func(x, y int, paths []string) {
		if a.service.State().Unlocked {
			runtime.EventsEmit(ctx, "vault:drop", paths)
		}
	})
	go func() {
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if a.service.State().Unlocked && runtime.WindowIsMinimised(ctx) {
					a.service.Lock()
				}
			}
		}
	}()
}
func (a *App) clearPreview() {
	a.previewMu.Lock()
	defer a.previewMu.Unlock()
	vc.Wipe(a.preview.Data)
	a.preview = vd.Preview{}
	a.previewToken = ""
}
func (a *App) event(e vd.Event) {
	if e.Kind == "locked" {
		a.clearPreview()
	}
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "vault:event", e)
	}
}
func (a *App) State() vd.State { return a.service.State() }
func (a *App) Unlock(secret string, recovery bool) error {
	return safe(a.service.Unlock(secret, recovery))
}

func (a *App) Lock()                                 { a.service.Lock(); a.clearPreview() }
func (a *App) Touch()                                { a.service.Touch() }
func (a *App) SetPreferences(p vd.Preferences) error { return safe(a.service.Preferences(p)) }
func (a *App) Create(password string) error {
	path, e := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{Title: "Save your recovery code", DefaultFilename: "vault-recovery.txt"})
	if e != nil {
		return errors.New("DIALOG_FAILED")
	}
	if path == "" {
		return errors.New("CANCELED")
	}
	return safe(a.service.Create(password, path))
}
func (a *App) OpenExisting(copyAccount bool) error {
	path, e := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{Title: "Select encrypted account file"})
	if e != nil {
		return errors.New("DIALOG_FAILED")
	}
	if path == "" {
		return nil
	}
	store := ""
	if !copyAccount {
		store = filepath.Join(filepath.Dir(path), "local.db")
		if _, e = os.Stat(store); os.IsNotExist(e) {
			store, e = runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{Title: "Select local.db (cancel to create an empty store)"})
			if e != nil {
				return errors.New("DIALOG_FAILED")
			}
		}
	}
	return safe(a.service.Select(path, store, copyAccount))
}
func (a *App) List() (vd.Library, error) { v, e := a.service.List(); return v, safe(e) }

func (a *App) Edit(revision string, edit vs.Edit) (string, error) {
	v, e := a.service.Edit(revision, edit)
	return v, safe(e)
}
func (a *App) Folder(name string, parent *string) (string, error) {
	v, e := a.service.Folder(name, parent)
	return v, safe(e)
}
func (a *App) Trash(revision string) (string, error) {
	v, e := a.service.Trash(revision)
	return v, safe(e)
}
func (a *App) Restore(revision string) (vd.Restored, error) {
	v, e := a.service.Restore(revision)
	return v, safe(e)
}
func (a *App) AddFiles(parent *string, tags []string) (string, error) {
	paths, e := runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{Title: "Add documents"})
	if e != nil {
		return "", errors.New("DIALOG_FAILED")
	}
	if len(paths) == 0 {
		return "", nil
	}
	v, e := a.service.Import(paths, parent, tags)
	return v, safe(e)
}
func (a *App) ImportDropped(paths []string, parent *string, tags []string) (string, error) {
	v, e := a.service.Import(paths, parent, tags)
	return v, safe(e)
}
func (a *App) Export(revision, name string) (string, error) {
	path, e := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{Title: "Export document", DefaultFilename: filepath.Base(name)})
	if e != nil {
		return "", errors.New("DIALOG_FAILED")
	}
	if path == "" {
		return "", nil
	}
	v, e := a.service.Export(revision, path)
	return v, safe(e)
}
func (a *App) ExportAccount() error {
	path, e := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{Title: "Save encrypted account file", DefaultFilename: "account.v1"})
	if e != nil {
		return errors.New("DIALOG_FAILED")
	}
	if path == "" {
		return nil
	}
	return safe(a.service.ExportAccount(path))
}
func (a *App) Cancel(op string) { a.service.Cancel(op) }
func (a *App) Connect(address string, register bool) error {
	return safe(a.service.Connect(address, register))
}
func (a *App) SyncNow() { a.service.SyncNow() }

type PreviewInfo struct {
	URL        string `json:"url"`
	MIME       string `json:"mime"`
	Generation uint64 `json:"generation"`
}

func (a *App) Preview(revision string) (PreviewInfo, error) {
	p, e := a.service.Preview(revision)
	if e != nil {
		return PreviewInfo{}, safe(e)
	}
	id, e := vc.RandomID()
	if e != nil {
		vc.Wipe(p.Data)
		return PreviewInfo{}, safe(e)
	}
	a.previewMu.Lock()
	defer a.previewMu.Unlock()
	st := a.service.State()
	if !st.Unlocked || st.Generation != p.Generation {
		vc.Wipe(p.Data)
		return PreviewInfo{}, errors.New("LOCKED")
	}
	vc.Wipe(a.preview.Data)
	a.preview = p
	a.previewToken = id.String()
	return PreviewInfo{"/preview/" + a.previewToken, p.MIME, p.Generation}, nil
}
func (a *App) ClosePreview() { a.clearPreview() }
func (a *App) serveHTTP(w http.ResponseWriter, r *http.Request) {
	a.previewMu.Lock()
	defer a.previewMu.Unlock()
	st := a.service.State()
	if r.Method != "GET" || a.previewToken == "" || r.URL.Path != "/preview/"+a.previewToken || !st.Unlocked || st.Generation != a.preview.Generation {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", a.preview.MIME)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(a.preview.Data)
}
func main() {
	root, e := os.UserConfigDir()
	if e != nil {
		os.Exit(1)
	}
	app := &App{}
	app.service, e = vd.New(filepath.Join(root, "Vault"), app.event)
	if e != nil {
		os.Exit(1)
	}
	defer app.service.Close()
	files, _ := fs.Sub(assets, "frontend/dist")
	e = wails.Run(&options.App{Title: "Vault", Width: 1200, Height: 820, MinWidth: 800, MinHeight: 600, BackgroundColour: &options.RGBA{R: 17, G: 18, B: 20, A: 255}, AssetServer: &assetserver.Options{Assets: files, Handler: http.HandlerFunc(app.serveHTTP)}, OnStartup: app.startup, OnShutdown: func(context.Context) { app.Lock() }, Bind: []interface{}{app}, SingleInstanceLock: &options.SingleInstanceLock{UniqueId: "vault-desktop-v1"}, DragAndDrop: &options.DragAndDrop{EnableFileDrop: true, DisableWebViewDrop: true}, Windows: &windows.Options{OnSuspend: app.Lock}, ErrorFormatter: func(err error) any { return err.Error() }})
	if e != nil {
		os.Exit(1)
	}
}
