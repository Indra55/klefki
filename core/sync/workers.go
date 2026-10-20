package sync

import (
	"context"
	"errors"
	"math/rand/v2"
	"strconv"
	"sync"
	"time"

	vc "klefki/core/crypto"
	"klefki/core/syncwire"
)

type transferGate struct {
	slots chan struct{}
	mu    sync.Mutex
	until time.Time
}

func (g *transferGate) acquire(ctx context.Context) (func(), error) {
	select {
	case g.slots <- struct{}{}:
		return func() { <-g.slots }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (g *transferGate) deferUntil(t time.Time) {
	g.mu.Lock()
	if t.After(g.until) {
		g.until = t
	}
	g.mu.Unlock()
}
func (g *transferGate) wait(ctx context.Context) error {
	for {
		g.mu.Lock()
		delay := time.Until(g.until)
		g.mu.Unlock()
		if delay <= 0 {
			return nil
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

type workTask struct {
	Revision           syncwire.Revision
	File, Direction    string
	Priority, Attempts int
}
type runningTask struct {
	task   workTask
	cancel context.CancelCauseFunc
}
type workResult struct {
	task workTask
	err  error
}

func (c *Client) syncQueue(ctx context.Context, settings syncwire.SyncSettings) error {
	var activeQueue bool
	localState, err := c.Store.RelayState()
	if err != nil {
		return err
	}
	if c.Store.db.QueryRow(`SELECT active FROM sync_queue WHERE id=1`).Scan(&activeQueue) == nil && activeQueue && localState.Epoch == settings.Epoch && localState.Mode == settings.Mode {
		needed, err := c.queueNeeded(ctx)
		if err != nil {
			return err
		}
		if !needed {
			return nil
		}
	}
	if err := c.Store.bind(c.Remote.base); err != nil {
		return err
	}
	if err := c.prepareRelay(ctx, false); err != nil {
		return err
	}
	if _, err := c.request(ctx, "POST", "/v1/sync/content/activate", nil, 1024); err != nil {
		return err
	}
	if _, err := c.Store.db.Exec(`UPDATE sync_queue SET active=1`); err != nil {
		return err
	}
	if _, err := c.Store.db.Exec(`UPDATE content_policy SET enabled=?,version=? WHERE id=1 AND version<=?`, settings.TrashDefault, settings.TrashPolicyVersion, settings.TrashPolicyVersion); err != nil {
		return err
	}
	c.gate = &transferGate{slots: make(chan struct{}, 4)}
	defer func() { c.gate = nil }()
	active := map[string]runningTask{}
	results := make(chan workResult, 2)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	var first error
	stop := func() {
		for _, r := range active {
			r.cancel(context.Canceled)
		}
		for len(active) > 0 {
			r := <-results
			delete(active, r.task.File)
		}
	}
	defer stop()
	cycle := true
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if cycle {
			if err := c.queueCycle(ctx); err != nil {
				return err
			}
			cycle = false
		}
		tasks, err := c.readyTasks()
		if err != nil {
			return err
		}
		for _, task := range tasks {
			if _, busy := active[task.File]; busy {
				continue
			}
			lower := false
			for _, running := range active {
				if running.task.Priority < task.Priority {
					lower = true
					break
				}
			}
			if lower {
				continue
			}
			if len(active) >= 2 {
				worst := ""
				priority := task.Priority
				for file, r := range active {
					if r.task.Priority > priority {
						priority = r.task.Priority
						worst = file
					}
				}
				if worst != "" {
					active[worst].cancel(errContentReplan)
				}
				break
			}
			if task.Priority >= 3 {
				busy := len(active) > 0
				for _, r := range active {
					if r.task.Priority < 3 {
						busy = true
					}
				}
				if busy {
					continue
				}
			}
			jobCtx, cancel := context.WithCancelCause(ctx)
			active[task.File] = runningTask{task, cancel}
			go func(task workTask) {
				retry := c.Retry
				if retry.Attempts == 0 {
					retry.Attempts = 1
				}
				worker := &Client{Store: c.Store, Remote: c.Remote, Session: c.Session, Master: c.Master, Retry: retry, Observe: c.report, OnContact: c.OnContact, OnChange: c.OnChange, OnDocument: c.OnDocument, gate: c.gate}
				err := worker.performTask(jobCtx, task)
				if errors.Is(context.Cause(jobCtx), errContentReplan) {
					err = errContentReplan
				}
				cancel(nil)
				results <- workResult{task, err}
			}(task)
		}
		if len(active) == 0 {
			if err = c.advanceContentCursor(); err != nil {
				return err
			}
			cursor, err := c.Store.Cursor()
			if err != nil {
				return err
			}
			if _, err = c.request(ctx, "PUT", "/v1/sync/cursor", syncwire.Cursor{Sequence: cursor}, 1024); err != nil {
				return err
			}
			if first != nil {
				return first
			}
			var waiting bool
			if c.Store.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM sync_tasks)`).Scan(&waiting) != nil {
				return ErrStore
			}
			if waiting {
				return ErrContentUnavailable
			}
			return c.finishTrashSync(ctx)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			cycle = true
		case result := <-results:
			delete(active, result.task.File)
			if result.err != nil {
				if errors.Is(result.err, errContentReplan) {
					_, err = c.Store.db.Exec(`UPDATE sync_tasks SET direction='plan' WHERE file=?`, result.task.File)
				} else {
					err = c.failTask(result.task, result.err)
					if first == nil {
						first = result.err
					}
					if globalWorkError(result.err) {
						return result.err
					}
				}
				if err != nil {
					return err
				}
			}
			if err = c.announceDocuments(); err != nil {
				return err
			}
			var local bool
			if c.Store.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM revisions WHERE pending=1) OR (SELECT reconcile FROM sync_queue WHERE id=1)`).Scan(&local) != nil {
				return ErrStore
			}
			cycle = local || errors.Is(result.err, errContentReplan)
		}
	}
}

func (c *Client) queueCycle(ctx context.Context) error {
	if err := c.syncDeletions(ctx); err != nil {
		return err
	}
	check, err := c.workCheck(ctx)
	if err != nil {
		return err
	}
	local, err := c.Store.RelayState()
	if err != nil {
		return err
	}
	if check.Epoch != local.Epoch || check.Mode != local.Mode {
		return errContentReplan
	}
	var initialized, reconcile bool
	var inventory int64
	if c.Store.db.QueryRow(`SELECT initialized,reconcile,inventory FROM sync_queue WHERE id=1`).Scan(&initialized, &reconcile, &inventory) != nil {
		return ErrStore
	}
	if !initialized {
		if _, err = c.Store.db.Exec(`INSERT OR IGNORE INTO sync_files SELECT id,lower(hex(substr(file_key,19,16))) FROM revisions WHERE length(file_key)=106;UPDATE sync_queue SET initialized=1`); err != nil {
			return err
		}
	}
	if inventory != check.Inventory {
		if err = c.publishInventory(ctx); err != nil {
			return err
		}
		reconcile = true
		if _, err = c.Store.db.Exec(`UPDATE sync_tasks SET retry_at=0,direction='plan',error='' WHERE error='capacity'`); err != nil {
			return err
		}
	}
	var policyVersion int64
	if c.Store.db.QueryRow(`SELECT version FROM content_policy WHERE id=1`).Scan(&policyVersion) != nil {
		return ErrStore
	}
	if policyVersion != check.Policy {
		settings, err := c.Settings(ctx)
		if err != nil {
			return err
		}
		if _, err = c.Store.db.Exec(`UPDATE content_policy SET enabled=?,version=? WHERE id=1`, settings.TrashDefault, settings.TrashPolicyVersion); err != nil {
			return err
		}
		reconcile = true
	}
	if err = c.publishPending(ctx); err != nil {
		return err
	}
	if err = c.pullWorkMetadata(ctx); err != nil {
		return err
	}
	// Folder moves can change the policy of descendants already present locally.
	var dirty bool
	if c.Store.db.QueryRow(`SELECT reconcile FROM sync_queue WHERE id=1`).Scan(&dirty) != nil {
		return ErrStore
	}
	if reconcile || dirty {
		if _, err = c.Store.db.Exec(`INSERT OR IGNORE INTO sync_tasks(revision,file) SELECT revision,file FROM sync_files;UPDATE sync_tasks SET direction='plan' WHERE retry_at=0;UPDATE sync_queue SET reconcile=0,inventory=?`, check.Inventory); err != nil {
			return err
		}
	}
	if err = c.pullContentWork(ctx); err != nil {
		return err
	}
	if err = c.planTasks(ctx); err != nil {
		return err
	}
	if err = c.advanceContentCursor(); err != nil {
		return err
	}
	var metadata, delivery int64
	if c.Store.db.QueryRow(`SELECT metadata_cursor FROM content_policy WHERE id=1`).Scan(&metadata) != nil {
		return ErrStore
	}
	delivery, err = c.Store.Cursor()
	if err != nil {
		return err
	}
	if _, err = c.request(ctx, "PUT", "/v1/sync/metadata-cursor", syncwire.Cursor{Sequence: metadata}, 1024); err != nil {
		return err
	}
	if _, err = c.request(ctx, "PUT", "/v1/sync/cursor", syncwire.Cursor{Sequence: delivery}, 1024); err != nil {
		return err
	}
	return c.announceDocuments()
}

func (c *Client) publishInventory(ctx context.Context) error {
	history, err := c.Store.contentHistory()
	if err != nil {
		return err
	}
	for start := 0; start < len(history); start += syncwire.RelayBatchSize {
		batch := history[start:min(start+syncwire.RelayBatchSize, len(history))]
		ids := []string{}
		byID := map[string]bool{}
		for _, r := range batch {
			ids = append(ids, r.ID)
			byID[r.ID] = true
		}
		b, err := c.request(ctx, "POST", "/v1/sync/relay/need-batch", syncwire.RelayNeed{IDs: ids}, 16384)
		if err != nil {
			return err
		}
		var needed syncwire.RelayNeed
		if parseJSON(b, &needed) != nil {
			return ErrInvalid
		}
		set := map[string]bool{}
		for _, id := range needed.IDs {
			if !byID[id] || set[id] {
				return ErrInvalid
			}
			set[id] = true
		}
		for _, r := range batch {
			if set[r.ID] {
				if err = c.publishMetadata(ctx, r); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
func (c *Client) publishPending(ctx context.Context) error {
	rows, err := c.Store.db.Query(`SELECT id,object,base,metadata,file_key FROM revisions WHERE pending=1 ORDER BY CASE WHEN base='' THEN 0 ELSE 1 END,ordinal`)
	if err != nil {
		return err
	}
	pending := []syncwire.Revision{}
	for rows.Next() {
		var r syncwire.Revision
		if rows.Scan(&r.ID, &r.Object, &r.Base, &r.Metadata, &r.FileKey) != nil {
			rows.Close()
			return ErrStore
		}
		pending = append(pending, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, r := range pending {
		if err = c.publishMetadata(ctx, r); err != nil {
			return err
		}
	}
	return nil
}
func (c *Client) publishMetadata(ctx context.Context, r syncwire.Revision) error {
	origin, err := c.Store.Origin(r.ID)
	if err != nil {
		return err
	}
	b, err := c.request(context.WithValue(ctx, originKey{}, origin), "PUT", "/v1/sync/revisions/"+r.ID, r, 1024)
	if err != nil {
		return err
	}
	var cursor syncwire.Cursor
	if parseJSON(b, &cursor) != nil || cursor.Sequence <= 0 {
		return ErrInvalid
	}
	return c.Store.uploaded(r.ID)
}
func (c *Client) pullWorkMetadata(ctx context.Context) error {
	var through *int64
	changed := false
	for {
		var cursor int64
		if c.Store.db.QueryRow(`SELECT metadata_cursor FROM content_policy WHERE id=1`).Scan(&cursor) != nil {
			return ErrStore
		}
		path := "/v1/sync/changes?after=" + strconv.FormatInt(cursor, 10)
		if through != nil {
			path += "&through=" + strconv.FormatInt(*through, 10)
		}
		b, err := c.request(ctx, "GET", path, nil, syncwire.MaxPageBytes)
		if err != nil {
			return err
		}
		var page syncwire.Page
		if parseJSON(b, &page) != nil || len(page.Changes) > syncwire.PageSize || page.Latest < cursor {
			return ErrInvalid
		}
		if through == nil {
			value := page.Latest
			through = &value
		}
		if len(page.Changes) == 0 {
			break
		}
		origins := c.provenance(ctx, cursor)
		for _, change := range page.Changes {
			changed = true
			if change.Sequence <= cursor || change.Sequence > *through {
				return ErrInvalid
			}
			deleted, err := c.Store.Deleted(change.Revision.Object)
			if err != nil {
				return err
			}
			if deleted {
				tx, err := c.Store.db.Begin()
				if err != nil {
					return err
				}
				if err = recordMetadataReceipt(tx, change); err != nil {
					tx.Rollback()
					return err
				}
				if err = tx.Commit(); err != nil {
					return err
				}
				cursor = change.Sequence
				continue
			}
			if len(change.Revision.Metadata) == 0 {
				saved, err := c.Store.localRevision(change.Revision.ID)
				if err != nil {
					return ErrRelayWaiting
				}
				if saved.Object != change.Revision.Object || saved.Base != change.Revision.Base {
					return ErrInvalid
				}
				change.Revision = saved
			}
			if err = c.Store.applyTracked(context.WithValue(ctx, metadataOnlyKey{}, true), c.Master, change, origins[change.Revision.ID].Origin, nil, nil); err != nil {
				return err
			}
			cursor = change.Sequence
		}
		if cursor >= *through {
			break
		}
	}
	_, err := c.Store.db.Exec(`UPDATE sync_queue SET baseline=1`)
	if err == nil && changed && c.OnChange != nil {
		c.OnChange()
	}
	return err
}
func (c *Client) pullContentWork(ctx context.Context) error {
	var through *int64
	for {
		var cursor int64
		if c.Store.db.QueryRow(`SELECT content_cursor FROM sync_queue WHERE id=1`).Scan(&cursor) != nil {
			return ErrStore
		}
		path := "/v1/sync/content/changes?after=" + strconv.FormatInt(cursor, 10)
		if through != nil {
			path += "&through=" + strconv.FormatInt(*through, 10)
		}
		b, err := c.request(ctx, "GET", path, nil, 32768)
		if err != nil {
			return err
		}
		var page syncwire.ContentEvents
		if parseJSON(b, &page) != nil || page.Latest < cursor || len(page.Changes) > 256 {
			return ErrInvalid
		}
		if through == nil {
			latest := page.Latest
			through = &latest
		}
		tx, err := c.Store.db.Begin()
		if err != nil {
			return err
		}
		for _, change := range page.Changes {
			if change.Sequence <= cursor || change.Sequence > *through {
				tx.Rollback()
				return ErrInvalid
			}
			if _, err = vc.ParseID(change.File); err != nil {
				tx.Rollback()
				return err
			}
			if _, err = tx.Exec(`INSERT OR IGNORE INTO sync_tasks(revision,file) SELECT revision,file FROM sync_files WHERE file=?`, change.File); err != nil {
				tx.Rollback()
				return err
			}
			if _, err = tx.Exec(`UPDATE sync_tasks SET direction='plan',retry_at=0,error='' WHERE file=? AND error='source'`, change.File); err != nil {
				tx.Rollback()
				return err
			}
			cursor = change.Sequence
		}
		if len(page.Changes) == 0 {
			cursor = *through
		}
		if _, err = tx.Exec(`UPDATE sync_queue SET content_cursor=?`, cursor); err != nil {
			tx.Rollback()
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
		if cursor >= *through {
			break
		}
	}
	return nil
}

func (c *Client) readyTasks() ([]workTask, error) {
	rows, err := c.Store.db.Query(`SELECT r.id,r.object,r.base,r.metadata,r.file_key,t.file,t.direction,t.priority,t.attempts FROM sync_tasks t JOIN revisions r ON r.id=t.revision WHERE r.pending=0 AND t.retry_at<=? ORDER BY t.priority,CASE WHEN t.priority>=3 THEN r.ordinal END,r.ordinal DESC`, time.Now().UnixMilli())
	if err != nil {
		return nil, err
	}
	out := []workTask{}
	files := map[string]bool{}
	for rows.Next() {
		var t workTask
		if rows.Scan(&t.Revision.ID, &t.Revision.Object, &t.Revision.Base, &t.Revision.Metadata, &t.Revision.FileKey, &t.File, &t.Direction, &t.Priority, &t.Attempts) != nil {
			rows.Close()
			return nil, ErrStore
		}
		if !files[t.File] {
			out = append(out, t)
			files[t.File] = true
		}
	}
	err = rows.Err()
	rows.Close()
	return out, err
}
func (c *Client) planTasks(ctx context.Context) error {
	tasks, err := c.readyTasks()
	if err != nil {
		return err
	}
	plans := []workTask{}
	ids := []string{}
	for _, t := range tasks {
		if t.Direction == "plan" {
			plans = append(plans, t)
			ids = append(ids, t.Revision.ID)
		}
	}
	states, err := c.contentStates(ctx, ids)
	if err != nil {
		return err
	}
	for _, t := range plans {
		trash, err := c.Store.objectTrash(c.Master, t.Revision.Object, map[string]bool{})
		if err != nil {
			return err
		}
		policy, err := c.Store.TrashPolicy()
		if err != nil {
			return err
		}
		var explicit, history bool
		if c.Store.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM deferred_content d JOIN sync_files f ON f.revision=d.revision WHERE f.file=? AND d.explicit=1),EXISTS(SELECT 1 FROM revisions WHERE base=?)`, t.File, t.Revision.ID).Scan(&explicit, &history) != nil {
			return ErrStore
		}
		wanted := !trash || policy.Effective || explicit
		if _, err = c.Store.db.Exec(`UPDATE deferred_content SET wanted=?,trash=? WHERE revision IN (SELECT revision FROM sync_files WHERE file=?)`, wanted, trash, t.File); err != nil {
			return err
		}
		refs, err := c.fileReceipts(t.File)
		if err != nil {
			return err
		}
		if !wanted {
			if err = c.contentAction(ctx, "release", refs); err != nil {
				return err
			}
			if _, err = c.Store.db.Exec(`DELETE FROM sync_tasks WHERE file=?;DELETE FROM content_work WHERE revision IN(SELECT revision FROM sync_files WHERE file=?)`, t.File, t.File); err != nil {
				return err
			}
			continue
		}
		t.Priority = 0
		if t.Revision.Base != "" {
			t.Priority = 1
		}
		if history || t.Attempts > 0 {
			t.Priority = 2
		}
		if trash && !explicit {
			t.Priority = 3
		}
		have, err := c.Store.ContentAvailable(c.Master, t.Revision.ID)
		if err != nil {
			return err
		}
		state := states[t.Revision.ID]
		t.Direction = "receipt"
		if !have || !state.Available && state.Requested {
			if err = c.contentAction(ctx, "request", refs); err != nil {
				return err
			}
			t.Direction = "download"
			if have {
				t.Direction = "upload"
			}
		}
		if _, err = c.Store.db.Exec(`UPDATE sync_tasks SET direction=?,priority=?,upload_pending=? WHERE file=?`, t.Direction, t.Priority, t.Direction == "upload", t.File); err != nil {
			return err
		}
		if !have && !state.Available {
			if err = c.failTask(t, ErrContentUnavailable); err != nil {
				return err
			}
		}
	}
	return nil
}
func (c *Client) fileReceipts(file string) ([]string, error) {
	rows, err := c.Store.db.Query(`SELECT f.revision FROM sync_files f JOIN revisions r ON r.id=f.revision WHERE f.file=? AND r.pending=0 ORDER BY r.ordinal`, file)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if rows.Scan(&id) != nil {
			rows.Close()
			return nil, ErrStore
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	return ids, err
}
func (c *Client) performTask(ctx context.Context, t workTask) error {
	guard := func() error {
		trash, err := c.Store.objectTrash(c.Master, t.Revision.Object, map[string]bool{})
		if err != nil {
			return err
		}
		if deleted, err := c.Store.Deleted(t.Revision.Object); err != nil {
			return err
		} else if deleted {
			return errContentReplan
		}
		p, err := c.Store.TrashPolicy()
		if err != nil {
			return err
		}
		var explicit bool
		_ = c.Store.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM deferred_content d JOIN sync_files f ON f.revision=d.revision WHERE f.file=? AND d.explicit=1)`, t.File).Scan(&explicit)
		if trash && !p.Effective && !explicit {
			return errContentReplan
		}
		var queued bool
		if c.Store.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM revisions WHERE pending=1) OR EXISTS(SELECT 1 FROM sync_tasks WHERE file<>? AND retry_at<=? AND priority<?)`, t.File, time.Now().UnixMilli(), t.Priority).Scan(&queued) != nil {
			return ErrStore
		}
		if queued {
			return errContentReplan
		}
		return nil
	}
	ctx = context.WithValue(ctx, contentGuardKey{}, guard)
	var err error
	if t.Direction == "upload" {
		err = c.push(ctx, t.Revision)
		if err == nil {
			err = c.contentAction(context.WithValue(ctx, contentGuardKey{}, nil), "available", []string{t.Revision.ID})
		}
	} else if t.Direction == "download" {
		err = c.fetchContent(ctx, t.Revision)
	}
	if err != nil {
		return err
	}
	if t.Direction == "download" {
		if c.OnChange != nil {
			c.OnChange()
		}
		if err = c.announceDocuments(); err != nil {
			return err
		}
	}
	refs, err := c.fileReceipts(t.File)
	if err != nil {
		return err
	}
	if err = c.contentAction(context.WithValue(ctx, contentGuardKey{}, nil), "received", refs); err != nil {
		return err
	}
	_, err = c.Store.db.Exec(`DELETE FROM sync_tasks WHERE file=? AND revision IN(SELECT id FROM revisions WHERE pending=0);DELETE FROM content_work WHERE revision IN(SELECT revision FROM sync_files WHERE file=?)`, t.File, t.File)
	return err
}
func (c *Client) failTask(t workTask, err error) error {
	code := "transfer"
	retry := time.Now().Add(min(5*time.Second*time.Duration(1<<min(t.Attempts, 6)), 5*time.Minute) + time.Duration(rand.Int64N(1000))*time.Millisecond).UnixMilli()
	if errors.Is(err, ErrContentUnavailable) {
		code = "source"
	}
	var h *HTTPError
	if errors.As(err, &h) && h.Status == 507 {
		code = "capacity"
	}
	if errors.Is(err, ErrInvalid) || errors.Is(err, vc.ErrAuth) || errors.Is(err, ErrConflict) {
		code = "integrity"
		retry = 9007199254740991
	}
	if errors.Is(err, context.Canceled) {
		return err
	}
	_, e := c.Store.db.Exec(`UPDATE sync_tasks SET direction='plan',attempts=attempts+1,retry_at=?,error=?,priority=CASE WHEN priority<3 THEN 2 ELSE priority END WHERE file=?`, retry, code, t.File)
	if e == nil {
		m, _, key, unlockErr := c.Store.unlock(c.Master, t.Revision)
		vc.Wipe(key[:])
		if unlockErr == nil {
			c.report(syncwire.Progress{Revision: t.Revision.ID, Name: m.Name, Direction: t.Direction, Phase: "waiting", Attempt: t.Attempts + 1, RetryAt: retry})
		}
	}
	return e
}
