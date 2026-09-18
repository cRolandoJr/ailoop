package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/cRolandoJr/ailoop/internal/config"
	"github.com/cRolandoJr/ailoop/internal/state"
	"github.com/cRolandoJr/ailoop/internal/verify"
)

func repoConTrabajoSinCommitear(t *testing.T) string {
	t.Helper()
	ws := t.TempDir()
	corre := func(args ...string) {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = ws
		c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	corre("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(ws, "importante.go"), []byte("// comiteado\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	corre("add", "importante.go")
	corre("commit", "-qm", "init")
	// Lo que el usuario tiene sin commitear, que es lo que hay que no perder.
	if err := os.WriteFile(filepath.Join(ws, "importante.go"), []byte("// TRABAJO SIN COMMITEAR\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return ws
}

// El peligro real: si el directorio del sandbox existe pero NO es un worktree,
// `git reset --hard` con cwd ahi descubre el repo PADRE hacia arriba y borra el
// trabajo sin commitear del usuario. Reproducido a mano antes de escribir esto.
func TestNoResetearUnDirectorioQueNoEsWorktree(t *testing.T) {
	ws := repoConTrabajoSinCommitear(t)
	sandbox := filepath.Join(ws, ".ailoop", "branches", "main", "sandbox")
	if err := os.MkdirAll(sandbox, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := prepareSandbox(ws, sandbox); err == nil {
		t.Error("acepto un directorio que no es worktree, en vez de negarse")
	}

	data, _ := os.ReadFile(filepath.Join(ws, "importante.go"))
	if string(data) != "// TRABAJO SIN COMMITEAR\n" {
		t.Fatalf("SE COMIO EL TRABAJO DEL USUARIO: %q", string(data))
	}
}

// Control positivo: sobre un worktree de verdad tiene que preparar y limpiar,
// o el guard de arriba estaria simplemente apagando la feature.
func TestSobreUnWorktreeDeVerdadPreparaYLimpia(t *testing.T) {
	ws := repoConTrabajoSinCommitear(t)
	sandbox := filepath.Join(ws, ".ailoop", "branches", "main", "sandbox")

	if err := prepareSandbox(ws, sandbox); err != nil {
		t.Fatalf("no pudo crear el worktree: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sandbox, "importante.go")); err != nil {
		t.Fatalf("el worktree quedo sin el checkout: %v", err)
	}

	// Suciedad de una corrida anterior: la segunda pasada tiene que limpiarla.
	basura := filepath.Join(sandbox, "basura.go")
	if err := os.WriteFile(basura, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := prepareSandbox(ws, sandbox); err != nil {
		t.Fatalf("segunda pasada: %v", err)
	}
	if _, err := os.Stat(basura); !os.IsNotExist(err) {
		t.Error("no limpio lo que habia quedado de la corrida anterior")
	}
}

// Un proyecto sin git no puede fallar en silencio: hoy los tres comandos van
// con `_ = cmd.Run()` y la rama entera se saltea sin decir nada.
func TestSinRepositorioGitLoDiceEnVezDeCallarse(t *testing.T) {
	ws := t.TempDir()
	if err := prepareSandbox(ws, filepath.Join(ws, "sandbox")); err == nil {
		t.Error("un proyecto sin git no dio error")
	}
}

// Los dos auto-fix compartian AutoRetries: el del sandbox (antes de la
// revision) y el del gate de DONE. Si el sandbox quemaba los 3, el gate real
// llegaba sin reintentos y el loop se plantaba en VERIFICATION.
func TestElSandboxNoGastaLosReintentosDelGateDeDone(t *testing.T) {
	ws := repoConTrabajoSinCommitear(t)

	s := state.NewState("tarea")
	s.CurrentPhase = state.PhaseImplementation
	s.Spec = state.Document{Version: 1, Content: "s", Approved: true}
	s.Design = state.Document{Version: 1, Content: "d", Approved: true}
	s.Plan = state.Document{Version: 1, Content: "p", Approved: true}
	s.NoteFileRead("importante.go")
	if err := state.Save(ws, s); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Verify:         []verify.Check{{Name: "falla", Cmd: "exit 1"}},
		TimeoutSeconds: 10,
	}
	if err := config.Save(ws, cfg); err != nil {
		t.Fatal(err)
	}

	parche := "<<<<\nimportante.go\n====\n// comiteado\n====\n// parcheado\n>>>>\n"
	m := &modeloScript{respuestas: []string{parche, parche, parche, parche}}
	l := NewLoop(ws, m, nil, cfg, nil)

	_, _ = l.Advance(context.Background(), AdvanceOptions{
		Review: rechazaCon("basta"),
	})

	cargado, err := state.Load(ws)
	if err != nil {
		t.Fatal(err)
	}
	if cargado.SandboxRetries == 0 {
		t.Error("el sandbox no llevo su propia cuenta")
	}
	if cargado.AutoRetries != 0 {
		t.Errorf("el sandbox gasto %d reintentos del gate de DONE", cargado.AutoRetries)
	}
}
