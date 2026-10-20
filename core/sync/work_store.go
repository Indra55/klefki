package sync

import (
	"context"
	"database/sql"
	"errors"
	"time"

	vc "klefki/core/crypto"
	"klefki/core/syncwire"
)

type DocumentEvent struct {
	Revision string `json:"revision"`
	Name     string `json:"name"`
	Source   string `json:"source,omitempty"`
	Stage    string `json:"stage"`
	Created  int64  `json:"created"`
	Read     bool   `json:"read"`
}

func (s *Store) migrateWork() error {
	_, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS sync_queue(id INTEGER PRIMARY KEY CHECK(id=1),active INTEGER NOT NULL DEFAULT 0,initialized INTEGER NOT NULL DEFAULT 0,content_cursor INTEGER NOT NULL DEFAULT 0,inventory INTEGER NOT NULL DEFAULT -1,reconcile INTEGER NOT NULL DEFAULT 1,baseline INTEGER NOT NULL DEFAULT 0);
 INSERT OR IGNORE INTO sync_queue(id) VALUES(1);
 CREATE TABLE IF NOT EXISTS sync_files(revision TEXT PRIMARY KEY,file TEXT NOT NULL);
 CREATE INDEX IF NOT EXISTS sync_files_file ON sync_files(file);
 CREATE TABLE IF NOT EXISTS sync_tasks(revision TEXT PRIMARY KEY,file TEXT NOT NULL,direction TEXT NOT NULL DEFAULT 'plan',priority INTEGER NOT NULL DEFAULT 0,attempts INTEGER NOT NULL DEFAULT 0,retry_at INTEGER NOT NULL DEFAULT 0,error TEXT NOT NULL DEFAULT '',upload_pending INTEGER NOT NULL DEFAULT 0);
 CREATE INDEX IF NOT EXISTS sync_tasks_ready ON sync_tasks(retry_at,priority);
 CREATE INDEX IF NOT EXISTS revisions_pending ON revisions(pending,ordinal);
 CREATE INDEX IF NOT EXISTS revisions_object ON revisions(object);
 CREATE TABLE IF NOT EXISTS sync_incoming(revision TEXT PRIMARY KEY,source TEXT NOT NULL,created INTEGER NOT NULL,seen INTEGER NOT NULL DEFAULT 0,reported INTEGER NOT NULL DEFAULT 0);
 CREATE TRIGGER IF NOT EXISTS sync_file_insert AFTER INSERT ON revisions WHEN length(NEW.file_key)=106 BEGIN
 INSERT OR IGNORE INTO sync_files VALUES(NEW.id,lower(hex(substr(NEW.file_key,19,16))));
 INSERT OR IGNORE INTO sync_tasks(revision,file,upload_pending) VALUES(NEW.id,lower(hex(substr(NEW.file_key,19,16))),NEW.pending); END;
 CREATE TRIGGER IF NOT EXISTS sync_folder_change AFTER INSERT ON revisions WHEN NEW.base<>'' AND NEW.file_key IS NULL BEGIN UPDATE sync_queue SET reconcile=1; END;
 CREATE TRIGGER IF NOT EXISTS sync_work_delete AFTER DELETE ON revisions BEGIN DELETE FROM sync_tasks WHERE revision=OLD.id;DELETE FROM sync_files WHERE revision=OLD.id;DELETE FROM sync_incoming WHERE revision=OLD.id; END;
 CREATE TRIGGER IF NOT EXISTS sync_policy_change AFTER UPDATE OF override,enabled,version ON content_policy WHEN OLD.override<>NEW.override OR OLD.enabled<>NEW.enabled OR OLD.version<>NEW.version BEGIN UPDATE sync_queue SET reconcile=1;END;`)
	return err
}

func (s *Store) trackIncoming(tx *sql.Tx, r syncwire.Revision, source string) error {
	if len(r.FileKey) == 0 {
		return nil
	}
	var baseline, known bool
	var device string
	if tx.QueryRow(`SELECT baseline FROM sync_queue WHERE id=1`).Scan(&baseline) != nil {
		return ErrStore
	}
	if !baseline {
		return nil
	}
	if tx.QueryRow(`SELECT device FROM relay_local WHERE id=1`).Scan(&device) != nil {
		return ErrStore
	}
	if source == device {
		return nil
	}
	if tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM revisions WHERE object=?)`, r.Object).Scan(&known) != nil {
		return ErrStore
	}
	if known {
		return nil
	}
	_, err := tx.Exec(`INSERT OR IGNORE INTO sync_incoming(revision,source,created) VALUES(?,?,?)`, r.ID, source, time.Now().UnixMilli())
	return err
}

func (s *Store) Incoming(master vc.Key) ([]DocumentEvent, error) {
	defer vc.Wipe(master[:])
	rows, err := s.db.Query(`SELECT i.revision,i.source,i.created,i.seen FROM sync_incoming i JOIN revisions r ON r.id=i.revision WHERE NOT EXISTS(SELECT 1 FROM deferred_content d WHERE d.revision=i.revision) ORDER BY i.created DESC LIMIT 100`)
	if err != nil {
		return nil, ErrStore
	}
	result := []DocumentEvent{}
	for rows.Next() {
		var x DocumentEvent
		if rows.Scan(&x.Revision, &x.Source, &x.Created, &x.Read) != nil {
			rows.Close()
			return nil, ErrStore
		}
		result = append(result, x)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for i := range result {
		r, err := s.Revision(master, result[i].Revision)
		if err != nil {
			return nil, err
		}
		result[i].Name = r.Metadata.Name
		result[i].Stage = "ready"
	}
	return result, nil
}
func (s *Store) ReadIncoming() error {
	_, err := s.db.Exec(`UPDATE sync_incoming SET seen=1 WHERE NOT EXISTS(SELECT 1 FROM deferred_content d WHERE d.revision=sync_incoming.revision)`)
	return err
}

func (s *Store) RetryFailed() error {
	_, err := s.db.Exec(`UPDATE sync_tasks SET retry_at=0,direction='plan',error=''`)
	return err
}

func (s *Store) WorkActivity(master vc.Key) ([]syncwire.Progress, error) {
	defer vc.Wipe(master[:])
	rows, err := s.db.Query(`SELECT revision,attempts,retry_at,error FROM sync_tasks WHERE error<>'' ORDER BY retry_at LIMIT 50`)
	if err != nil {
		return nil, err
	}
	out := []syncwire.Progress{}
	for rows.Next() {
		var p syncwire.Progress
		var code string
		if rows.Scan(&p.Revision, &p.Attempt, &p.RetryAt, &code) != nil {
			rows.Close()
			return nil, ErrStore
		}
		p.Phase = "error"
		if code == "source" {
			p.Phase = "waiting"
		}
		p.UpdatedAt = time.Now().UnixMilli()
		out = append(out, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for i := range out {
		record, err := s.Revision(master, out[i].Revision)
		if err != nil {
			return nil, err
		}
		out[i].Name = record.Metadata.Name
		have, err := s.ContentAvailable(master, out[i].Revision)
		if err != nil {
			return nil, err
		}
		out[i].Direction = "download"
		if have {
			out[i].Direction = "upload"
		}
	}
	return out, nil
}

func (c *Client) announceDocuments() error {
	items, err := c.Store.Incoming(c.Master)
	if err != nil {
		return err
	}
	for _, x := range items {
		result, err := c.Store.db.Exec(`UPDATE sync_incoming SET reported=1 WHERE revision=? AND reported=0`, x.Revision)
		if err != nil {
			return err
		}
		if n, _ := result.RowsAffected(); n > 0 && c.OnDocument != nil {
			c.OnDocument(x)
		}
	}
	return nil
}

func (s *Store) objectTrash(master vc.Key, object string, seen map[string]bool) (bool, error) {
	if seen[object] {
		return false, nil
	}
	seen[object] = true
	rows, err := s.db.Query(`SELECT r.id,r.object,r.base,r.metadata,r.file_key FROM revisions r WHERE r.object=? AND NOT EXISTS(SELECT 1 FROM revisions c WHERE c.base=r.id)`, object)
	if err != nil {
		return false, ErrStore
	}
	branches := []syncwire.Revision{}
	for rows.Next() {
		var r syncwire.Revision
		if rows.Scan(&r.ID, &r.Object, &r.Base, &r.Metadata, &r.FileKey) != nil {
			rows.Close()
			return false, ErrStore
		}
		branches = append(branches, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return false, err
	}
	if len(branches) == 0 {
		return false, nil
	}
	for _, r := range branches {
		m, _, key, err := s.unlock(master, r)
		vc.Wipe(key[:])
		if err != nil {
			return false, err
		}
		if trashRoot(m) {
			continue
		}
		if m.Parent == nil {
			return false, nil
		}
		copy := map[string]bool{}
		for k, v := range seen {
			copy[k] = v
		}
		trash, err := s.objectTrash(master, *m.Parent, copy)
		if err != nil || !trash {
			return false, err
		}
	}
	return true, nil
}

func (c *Client) queueNeeded(ctx context.Context) (bool, error) {
	var initialized, local bool
	var content, inventory, metadata, deletion, policy int64
	if c.Store.db.QueryRow(`SELECT initialized,content_cursor,inventory,reconcile FROM sync_queue WHERE id=1`).Scan(&initialized, &content, &inventory, &local) != nil {
		return true, ErrStore
	}
	if !initialized || local {
		return true, nil
	}
	if c.Store.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM revisions WHERE pending=1) OR EXISTS(SELECT 1 FROM deletions WHERE pending=1) OR EXISTS(SELECT 1 FROM sync_tasks WHERE retry_at<=?)`, time.Now().UnixMilli()).Scan(&local) != nil {
		return true, ErrStore
	}
	if local {
		return true, nil
	}
	if c.Store.db.QueryRow(`SELECT metadata_cursor,version FROM content_policy WHERE id=1`).Scan(&metadata, &policy) != nil || c.Store.db.QueryRow(`SELECT cursor FROM deletion_state WHERE id=1`).Scan(&deletion) != nil {
		return true, ErrStore
	}
	check, err := c.workCheck(ctx)
	if err != nil {
		return true, err
	}
	relay, err := c.Store.RelayState()
	if err != nil {
		return true, err
	}
	return check.Epoch != relay.Epoch || check.Mode != relay.Mode || check.Metadata != metadata || check.Content != content || check.Inventory != inventory || check.Deletion != deletion || check.Policy != policy, nil
}

func (c *Client) workCheck(ctx context.Context) (syncwire.WorkCheck, error) {
	var out syncwire.WorkCheck
	b, err := c.request(ctx, "GET", "/v1/sync/check", nil, 2048)
	if err != nil {
		return out, err
	}
	if parseJSON(b, &out) != nil {
		return out, ErrInvalid
	}
	if _, err = vc.ParseID(out.Epoch); err != nil || out.Metadata < 0 || out.Content < 0 || out.Deletion < 0 || out.Inventory < 0 || out.Policy < 1 || (out.Mode != syncwire.Persistent && out.Mode != syncwire.TemporaryRelay) {
		return out, ErrInvalid
	}
	return out, nil
}

func globalWorkError(err error) bool {
	var h *HTTPError
	return errors.Is(err, ErrRemote) || errors.As(err, &h) && (h.Status == 401 || h.Status == 403 || h.Status == 429 || h.Status == 428)
}
