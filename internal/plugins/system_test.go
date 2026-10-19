package plugins

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | PS-01 | Happy    | mo.run takes argv without a shell, cwd, env and stdin, and keeps stdout and stderr apart | P0 |
// | PS-02 | Error    | mo.run with a timeout stops the command and says so                       | P1       |
// | PS-03 | Happy    | mo.http gets, posts a body or a table as JSON, with headers, and reads status and headers | P0 |
// | PS-04 | Edge     | A 404 is an answer, not an error; a refused connection gives nil and the reason | P0  |
// | PS-05 | Security | A server signed by a private authority needs ca or insecure; redirects = false stops at the first answer | P0 |
// | PS-06 | Happy    | mo.fs lists, stats, makes, renames, globs and removes                    | P0       |
// | PS-07 | Happy    | require finds modules in the lib folder beside the plugins               | P0       |
// | PS-08 | Happy    | base64, sha256, hmac and url escaping match the standard ones            | P1       |
// | PS-09 | Edge     | os.execute runs with no terminal, output in the log, and gives the exit status | P1 |
// | PS-10 | Edge     | mo.sleep stops when the call is stopped                                  | P1       |
// | PS-11 | Happy    | mo.http.listen takes the one request to its path, gives its query, answers the browser, and closes | P0 |
// | PS-12 | Edge     | A listener that is waited on past its time gives up with the reason      | P1       |

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jg/mo/internal/tools"
)

// loadTree is a config folder with plugins/ and lib/, the plugins and the
// modules given by name.
func loadTree(t *testing.T, plugins, libs map[string]string) fixture {
	t.Helper()
	root := t.TempDir()
	for folder, files := range map[string]map[string]string{"plugins": plugins, "lib": libs} {
		for name, source := range files {
			path := filepath.Join(root, folder, name+".lua")
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	f := fixture{workDir: t.TempDir(), logDir: t.TempDir()}
	f.set = Load(filepath.Join(root, "plugins"), f.workDir, f.logDir)
	t.Cleanup(f.set.Close)
	return f
}

// runTool runs the first tool of the fixture and reads its answer as JSON.
func runTool(t *testing.T, f fixture) map[string]any {
	t.Helper()
	output, err := f.set.Tools(nil)[0].Run(context.Background(), tools.Env{}, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("tool failed: %v", err)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("answer is not JSON: %q", output)
	}
	return result
}

func TestProcesses(t *testing.T) {
	t.Run("PS-01 mo.run takes argv without a shell, cwd, env and stdin, and keeps stdout and stderr apart", func(t *testing.T) {
		f := loadSource(t, map[string]string{"run": `
mo.tool{ name = "go", run = function()
  mo.fs.mkdir("sub")
  local shell = mo.run{ command = "echo out; echo err >&2; exit 4" }
  local argv = mo.run{ argv = { "sh", "-c", 'echo "$GREETING $(cat) in $(basename "$PWD")"' }, env = { GREETING = "hi" }, stdin = "there", cwd = "sub" }
  local noshell = mo.run{ argv = { "echo", "$HOME;" } }
  return { shell = shell, argv = argv.stdout, noshell = noshell.stdout }
end }`})

		result := runTool(t, f)

		shell := result["shell"].(map[string]any)
		if shell["stdout"] != "out\n" || shell["stderr"] != "err\n" || shell["code"] != float64(4) || shell["ok"] != false {
			t.Errorf("shell = %v", shell)
		}
		if result["argv"] != "hi there in sub\n" || result["noshell"] != "$HOME;\n" {
			t.Errorf("argv = %q noshell = %q", result["argv"], result["noshell"])
		}
	})

	t.Run("PS-02 mo.run with a timeout stops the command and says so", func(t *testing.T) {
		f := loadSource(t, map[string]string{"run": `
mo.tool{ name = "slow", run = function() return mo.run{ command = "sleep 30", timeout = 0.2 } end }`})
		started := time.Now()

		result := runTool(t, f)

		if result["ok"] != false || time.Since(started) > 5*time.Second {
			t.Errorf("result = %v after %v", result, time.Since(started))
		}
	})

	t.Run("PS-09 os.execute runs with no terminal, output in the log, and gives the exit status", func(t *testing.T) {
		f := loadSource(t, map[string]string{"exec": `
mo.tool{ name = "go", run = function() return { good = os.execute("echo from execute"), bad = os.execute("exit 2") } end }`})

		result := runTool(t, f)

		log, _ := os.ReadFile(filepath.Join(f.logDir, "plugin-exec.log"))
		if result["good"] != float64(0) || result["bad"] != float64(2) || !strings.Contains(string(log), "from execute") {
			t.Errorf("result = %v log = %q", result, log)
		}
	})

	t.Run("PS-10 mo.sleep stops when the call is stopped", func(t *testing.T) {
		f := loadSource(t, map[string]string{"sleep": `
mo.tool{ name = "nap", run = function() mo.sleep(30) return "woke" end }`})
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		started := time.Now()

		_, err := f.set.Tools(nil)[0].Run(ctx, tools.Env{}, json.RawMessage(`{}`))

		if err == nil || time.Since(started) > 5*time.Second {
			t.Errorf("err = %v after %v", err, time.Since(started))
		}
	})
}

func TestNetwork(t *testing.T) {
	t.Run("PS-03 mo.http gets, posts a body or a table as JSON, with headers, and reads status and headers", func(t *testing.T) {
		web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			w.Header().Set("X-Seen", r.Method+" "+r.Header.Get("Content-Type")+" "+r.Header.Get("X-Token"))
			_, _ = w.Write(body)
		}))
		defer web.Close()
		f := loadSource(t, map[string]string{"web": `
mo.tool{ name = "go", run = function()
  local got = mo.http.get("` + web.URL + `", { headers = { ["X-Token"] = "t1" } })
  local posted = mo.http.post("` + web.URL + `", "plain body")
  local sent = mo.http.request{ url = "` + web.URL + `", json = { name = "ana", list = { 1, 2 } } }
  return { get = got.headers["x-seen"], status = got.status, ok = got.ok, post = posted.body, postSeen = posted.headers["x-seen"], json = mo.json_decode(sent.body), jsonSeen = sent.headers["x-seen"] }
end }`})

		result := runTool(t, f)

		if result["get"] != "GET  t1" || result["status"] != float64(200) || result["ok"] != true || result["post"] != "plain body" || result["postSeen"] != "POST" {
			t.Errorf("result = %v", result)
		}
		if result["jsonSeen"] != "POST application/json" || result["json"].(map[string]any)["name"] != "ana" {
			t.Errorf("json = %v seen = %v", result["json"], result["jsonSeen"])
		}
	})

	t.Run("PS-04 a 404 is an answer, not an error; a refused connection gives nil and the reason", func(t *testing.T) {
		web := httptest.NewServer(http.NotFoundHandler())
		url := web.URL
		web.Close()
		missing := httptest.NewServer(http.NotFoundHandler())
		defer missing.Close()
		f := loadSource(t, map[string]string{"web": `
mo.tool{ name = "go", run = function()
  local answer = mo.http.get("` + missing.URL + `")
  local response, why = mo.http.get("` + url + `", { timeout = 2 })
  return { status = answer.status, ok = answer.ok, refused = tostring(response), why = why }
end }`})

		result := runTool(t, f)

		if result["status"] != float64(404) || result["ok"] != false || result["refused"] != "nil" || !strings.Contains(result["why"].(string), "connect") {
			t.Errorf("result = %v", result)
		}
	})

	t.Run("PS-05 a server signed by a private authority needs ca or insecure; redirects = false stops at the first answer", func(t *testing.T) {
		web := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/old" {
				http.Redirect(w, r, "/new", http.StatusFound)
				return
			}
			_, _ = w.Write([]byte("secret"))
		}))
		web.Config.ErrorLog = log.New(io.Discard, "", 0)
		web.StartTLS()
		defer web.Close()
		caFile := filepath.Join(t.TempDir(), "ca.pem")
		writeCertificate(t, caFile, web)
		f := loadSource(t, map[string]string{"web": `
mo.tool{ name = "go", run = function()
  local plain, why = mo.http.get("` + web.URL + `")
  local trusted = mo.http.get("` + web.URL + `", { ca = "` + caFile + `" })
  local any = mo.http.get("` + web.URL + `", { insecure = true })
  local followed = mo.http.get("` + web.URL + `/old", { insecure = true })
  local stopped = mo.http.get("` + web.URL + `/old", { insecure = true, redirects = false })
  return { plain = tostring(plain), why = why, trusted = trusted.body, any = any.body, followed = followed.body, stopped = stopped.status, where = stopped.headers.location }
end }`})

		result := runTool(t, f)

		if result["plain"] != "nil" || !strings.Contains(result["why"].(string), "certificate") || result["trusted"] != "secret" || result["any"] != "secret" {
			t.Errorf("result = %v", result)
		}
		if result["followed"] != "secret" || result["stopped"] != float64(302) || result["where"] != "/new" {
			t.Errorf("redirects: %v", result)
		}
	})
}

func TestListening(t *testing.T) {
	t.Run("PS-11 mo.http.listen takes the one request to its path, gives its query, answers the browser, and closes", func(t *testing.T) {
		f := loadSource(t, map[string]string{"cb": `
mo.tool{ name = "go", run = function()
  local server = mo.http.listen{ path = "/callback", reply = "all done" }
  local url = "http://127.0.0.1:" .. server.port
  mo.run{ command = "(sleep 0.2; curl -s " .. url .. "/favicon.ico >/dev/null; curl -s '" .. url .. "/callback?code=abc&state=x%20y' > reply.txt) &" }
  local request = server.wait(5)
  mo.sleep(0.5)
  local again, why = mo.http.get(url .. "/callback", { timeout = 2 })
  return { code = request.query.code, state = request.query.state, path = request.path, method = request.method, reply = mo.read("reply.txt"), closed = tostring(again) }
end }`})

		result := runTool(t, f)

		if result["code"] != "abc" || result["state"] != "x y" || result["path"] != "/callback" || result["method"] != "GET" || result["reply"] != "all done" || result["closed"] != "nil" {
			t.Errorf("result = %v", result)
		}
	})

	t.Run("PS-12 a listener that is waited on past its time gives up with the reason", func(t *testing.T) {
		f := loadSource(t, map[string]string{"cb": `
mo.tool{ name = "go", run = function()
  local server = mo.http.listen()
  local ok, why = server.wait(0.2)
  return { got = tostring(ok), why = why }
end }`})

		result := runTool(t, f)

		if result["got"] != "nil" || !strings.Contains(result["why"].(string), "deadline") {
			t.Errorf("result = %v", result)
		}
	})
}

func writeCertificate(t *testing.T, path string, web *httptest.Server) {
	t.Helper()
	block := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: web.Certificate().Raw})
	if err := os.WriteFile(path, block, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestFiles(t *testing.T) {
	t.Run("PS-06 mo.fs lists, stats, makes, renames, globs and removes", func(t *testing.T) {
		f := loadSource(t, map[string]string{"fs": `
mo.tool{ name = "go", run = function()
  mo.fs.mkdir("a/b")
  mo.write("a/one.txt", "12345")
  mo.write("a/two.log", "x")
  local listed = mo.fs.list("a")
  local names = {}
  for _, entry in ipairs(listed) do names[#names + 1] = entry.name .. (entry.is_dir and "/" or "") end
  local stat = mo.fs.stat("a/one.txt")
  local globbed = mo.fs.glob("a/*.txt")
  mo.fs.rename("a/one.txt", "a/uno.txt")
  local renamed = mo.fs.stat("a/uno.txt") ~= nil
  local notEmpty, why = mo.fs.remove("a")
  local removed = mo.fs.remove("a", true)
  local missing, missingWhy = mo.fs.stat("a")
  return { names = names, size = stat.size, dir = stat.is_dir, globbed = #globbed, renamed = renamed, notEmpty = tostring(notEmpty), removed = removed, missing = tostring(missing) }
end }`})

		result := runTool(t, f)

		names := result["names"].([]any)
		if len(names) != 3 || names[0] != "b/" || names[1] != "one.txt" || names[2] != "two.log" {
			t.Errorf("names = %v", names)
		}
		if result["size"] != float64(5) || result["dir"] != false || result["globbed"] != float64(1) || result["renamed"] != true {
			t.Errorf("result = %v", result)
		}
		if result["notEmpty"] != "nil" || result["removed"] != true || result["missing"] != "nil" {
			t.Errorf("removing: %v", result)
		}
	})

	t.Run("PS-07 require finds modules in the lib folder beside the plugins", func(t *testing.T) {
		f := loadTree(t,
			map[string]string{"user": `
local greet = require("greet")
local deep = require("tools.shout")
mo.tool{ name = "go", run = function() return { plain = greet.hello("ana"), deep = deep("hey") } end }`},
			map[string]string{
				"greet":            `return { hello = function(name) return "hello " .. name end }`,
				"tools/shout/init": `return function(text) return text:upper() end`,
			})

		result := runTool(t, f)

		if result["plain"] != "hello ana" || result["deep"] != "HEY" {
			t.Errorf("result = %v", result)
		}
	})
}

func TestEncodings(t *testing.T) {
	t.Run("PS-08 base64, sha256, hmac and url escaping match the standard ones", func(t *testing.T) {
		f := loadSource(t, map[string]string{"enc": `
mo.tool{ name = "go", run = function()
  local decoded, why = mo.base64_decode("not base64!")
  return {
    encoded = mo.base64_encode("ana@x.com:token"),
    decoded = mo.base64_decode("YW5hQHguY29tOnRva2Vu"),
    bad = tostring(decoded),
    sha = mo.sha256("abc"),
    hmac = mo.hmac_sha256("key", "The quick brown fox jumps over the lazy dog"),
    url = mo.url_encode("a b&c=d/é"),
  }
end }`})

		result := runTool(t, f)

		want := map[string]string{
			"encoded": "YW5hQHguY29tOnRva2Vu",
			"decoded": "ana@x.com:token",
			"bad":     "nil",
			"sha":     "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
			"hmac":    "f7bc83f430538424b13298e6aa6fb143ef4d59a14946175997479dbc2d1a3cd8",
			"url":     "a+b%26c%3Dd%2F%C3%A9",
		}
		for name, value := range want {
			if result[name] != value {
				t.Errorf("%s = %q, want %q", name, result[name], value)
			}
		}
	})
}
