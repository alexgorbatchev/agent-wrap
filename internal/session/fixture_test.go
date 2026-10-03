package session

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alexgorbatchev/agent-watcher/scanner"
	_ "github.com/ncruces/go-sqlite3/driver"
)

func writeRecord(t *testing.T, path string, record map[string]interface{}) {
	t.Helper()
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// This real subprocess writes synthetic harness storage entirely in test dirs.
func TestWrappedChild(t *testing.T) {
	if os.Getenv("WRAP_TEST_CHILD") != "1" {
		return
	}
	harness := os.Getenv("WRAP_TEST_HARNESS")
	base, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	id := "11111111-1111-4111-8111-111111111111"
	path := ""
	var db *sql.DB
	switch harness {
	case "claude-code":
		path = filepath.Join(os.Getenv("CLAUDE_PROJECTS_DIR"), strings.NewReplacer("/", "-", ".", "-").Replace(base), id+".jsonl")
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		desc := map[string]interface{}{"pid": os.Getpid(), "sessionId": id, "cwd": base, "startedAt": now.UnixMilli(), "version": "test", "status": "busy"}
		writeRecord(t, filepath.Join(os.Getenv("CLAUDE_SESSIONS_DIR"), strconv.Itoa(os.Getpid())+".json"), desc)
		if err := os.WriteFile(path, nil, 0600); err != nil {
			t.Fatal(err)
		}
	case "pi":
		path = filepath.Join(os.Getenv("PI_SESSIONS_DIR"), scanner.EncodePiCWD(base), "session.jsonl")
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		writeRecord(t, path, map[string]interface{}{"type": "session", "version": 3, "id": id, "cwd": base, "timestamp": now.Format(time.RFC3339Nano)})
	case "codex":
		path = filepath.Join(os.Getenv("CODEX_SESSIONS_DIR"), "rollout-"+now.Format("2006-01-02T15-04-05")+"-"+id+".jsonl")
		writeRecord(t, path, map[string]interface{}{"type": "session_meta", "timestamp": now.Format(time.RFC3339Nano), "payload": map[string]interface{}{"id": id, "cwd": base, "timestamp": now.Format(time.RFC3339Nano), "source": "cli", "cli_version": "test"}})
	case "opencode":
		db, err = sql.Open("sqlite3", "file:"+os.Getenv("OPENCODE_DB_PATH")+"?mode=rwc")
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := db.Close(); err != nil {
				t.Error(err)
			}
		}()
		schema := `CREATE TABLE session (id TEXT PRIMARY KEY, project_id TEXT, parent_id TEXT, slug TEXT, directory TEXT NOT NULL, title TEXT, version TEXT, agent TEXT, model TEXT, cost REAL, tokens_input INTEGER DEFAULT 0, tokens_output INTEGER DEFAULT 0, tokens_reasoning INTEGER DEFAULT 0, tokens_cache_read INTEGER DEFAULT 0, tokens_cache_write INTEGER DEFAULT 0, time_created INTEGER, time_updated INTEGER, time_archived INTEGER, metadata TEXT);
CREATE TABLE message (id TEXT PRIMARY KEY,session_id TEXT,time_created INTEGER,data TEXT);
CREATE TABLE part (id TEXT PRIMARY KEY,message_id TEXT,session_id TEXT,time_created INTEGER,data TEXT);`
		if _, err := db.Exec(schema); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec("INSERT INTO session (id,directory,title,version,time_created,time_updated) VALUES (?,?,?,?,?,?)", id, base, "Task", "test", now.UnixMilli(), now.UnixMilli()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := fmt.Fprint(os.Stdout, "\x1b[2J\x1b[HFRAME CHILD ONLINE"); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewScanner(os.Stdin)
	index := 0
	for reader.Scan() {
		dir := reader.Text()
		if dir == "quit" {
			os.Exit(37)
		}
		if dir == "signal" {
			p, err := os.FindProcess(os.Getpid())
			if err != nil {
				t.Fatal(err)
			}
			if err := p.Signal(os.Interrupt); err != nil {
				t.Fatal(err)
			}
			continue
		}
		stamp := time.Now().UTC()
		index++
		call := fmt.Sprintf("call%d", index)
		input := map[string]interface{}{"file_path": filepath.Join(dir, "target.go"), "path": filepath.Join(dir, "target.go"), "filePath": filepath.Join(dir, "target.go")}
		switch harness {
		case "claude-code":
			writeRecord(t, path, map[string]interface{}{"type": "assistant", "sessionId": id, "uuid": call, "cwd": base, "timestamp": stamp.Format(time.RFC3339Nano), "message": map[string]interface{}{"role": "assistant", "id": call, "model": "test", "content": []interface{}{map[string]interface{}{"type": "tool_use", "id": call, "name": "Read", "input": input}}}})
		case "pi":
			writeRecord(t, path, map[string]interface{}{"type": "message", "id": call, "timestamp": stamp.Format(time.RFC3339Nano), "message": map[string]interface{}{"role": "assistant", "model": "test", "timestamp": stamp.UnixMilli(), "content": []interface{}{map[string]interface{}{"type": "toolCall", "id": call, "name": "read", "arguments": input}}}})
		case "codex":
			args, err := json.Marshal(map[string]interface{}{"cmd": "pwd", "workdir": dir})
			if err != nil {
				t.Fatal(err)
			}
			writeRecord(t, path, map[string]interface{}{"type": "response_item", "timestamp": stamp.Format(time.RFC3339Nano), "payload": map[string]interface{}{"type": "function_call", "name": "exec_command", "call_id": call, "arguments": string(args)}})
		case "opencode":
			if _, err := db.Exec("INSERT INTO message VALUES (?,?,?,?)", call, id, stamp.UnixMilli(), `{"role":"assistant"}`); err != nil {
				t.Fatal(err)
			}
			part, err := json.Marshal(map[string]interface{}{"type": "tool", "tool": "read", "callID": call, "state": map[string]interface{}{"status": "running", "input": input, "time": map[string]interface{}{"start": stamp.UnixMilli()}}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec("INSERT INTO part VALUES (?,?,?,?,?)", call, call, id, stamp.UnixMilli(), string(part)); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := fmt.Fprint(os.Stdout, "\x1b[2J\x1b[HCHILD UPDATED"); err != nil {
			t.Fatal(err)
		}
	}
	if err := reader.Err(); err != nil {
		t.Fatal(err)
	}
}
