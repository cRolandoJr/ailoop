package env

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func repo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("sin git")
	}
	ws := t.TempDir()
	// -b main: sin el flag, la rama inicial depende del init.defaultBranch
	// del HOST (en el sandbox de nix, sin config global, es master) y el test
	// que espera "branches/main" falla segun donde corra.
	git(t, ws, "init", "-q", "-b", "main", ".")
	git(t, ws, "config", "user.email", "t@t")
	git(t, ws, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(ws, "f.txt"), []byte("x\n"), 0644); err != nil {
		t.Fatal(err)
	}
	git(t, ws, "add", "f.txt")
	git(t, ws, "commit", "-qm", "base")
	return ws
}

func arbol(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	filepath.Walk(root, func(p string, i os.FileInfo, e error) error {
		if e != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		if rel != "." {
			out = append(out, rel)
		}
		return nil
	})
	return out
}

func TestAILoopDirNoTocaElDisco(t *testing.T) {
	// Es una consulta. La version anterior movia archivos del usuario al ser
	// llamada, y se la llamaba en cada Load, cada Save y cada backup.
	ws := repo(t)
	if err := os.MkdirAll(filepath.Join(ws, ".ailoop"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, ".ailoop", "state.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	antes := arbol(t, filepath.Join(ws, ".ailoop"))
	for i := 0; i < 3; i++ {
		AILoopDir(ws)
	}
	despues := arbol(t, filepath.Join(ws, ".ailoop"))

	if strings.Join(antes, ",") != strings.Join(despues, ",") {
		t.Errorf("consultar la ruta cambio el disco:\n  antes:   %v\n  despues: %v", antes, despues)
	}
}

func TestLaRutaSigueALaRama(t *testing.T) {
	ws := repo(t)
	enMain := AILoopDir(ws)
	if !strings.Contains(enMain, filepath.Join("branches", "main")) {
		t.Errorf("en main: %s", enMain)
	}

	git(t, ws, "checkout", "-q", "-b", "feature/algo")
	enFeature := AILoopDir(ws)
	if !strings.Contains(enFeature, filepath.Join("branches", "feature_algo")) {
		t.Errorf("la barra no se saneo: %s", enFeature)
	}
}

func TestHeadDetachedYSinRepoVanADefault(t *testing.T) {
	// Un worktree desechable queda detached, y ahi no hay linea de trabajo
	// a la que pertenecer.
	ws := repo(t)
	git(t, ws, "checkout", "-q", "--detach", "HEAD")
	if !strings.HasSuffix(AILoopDir(ws), filepath.Join(".ailoop", "default")) {
		t.Errorf("detached: %s", AILoopDir(ws))
	}

	sinRepo := t.TempDir()
	if !strings.HasSuffix(AILoopDir(sinRepo), filepath.Join(".ailoop", "default")) {
		t.Errorf("sin repo: %s", AILoopDir(sinRepo))
	}
}

func TestSanitiseNoDejaEscaparDeLaCarpeta(t *testing.T) {
	for _, malo := range []string{"../../etc", "..", "a/../../b", ".", "  "} {
		got := sanitise(malo)
		if strings.Contains(got, "..") {
			t.Errorf("sanitise(%q) = %q, deja escapar", malo, got)
		}
	}
	if got := sanitise("feature/x"); got != "feature_x" {
		t.Errorf("sanitise = %q", got)
	}
}

func TestMigrateMueveElEstadoYReportaQueLoHizo(t *testing.T) {
	ws := repo(t)
	if err := os.MkdirAll(filepath.Join(ws, ".ailoop"), 0755); err != nil {
		t.Fatal(err)
	}
	contenido := `{"task_description":"trabajo importante"}`
	if err := os.WriteFile(filepath.Join(ws, ".ailoop", "state.json"), []byte(contenido), 0644); err != nil {
		t.Fatal(err)
	}

	moved, err := Migrate(ws)
	if err != nil {
		t.Fatal(err)
	}
	if !moved {
		t.Error("no informo que migro")
	}

	data, err := os.ReadFile(filepath.Join(AILoopDir(ws), "state.json"))
	if err != nil {
		t.Fatalf("el estado no llego a la carpeta de la rama: %v", err)
	}
	if string(data) != contenido {
		t.Errorf("el contenido cambio: %q", string(data))
	}
}

func TestMigrateNoTocaLosBackups(t *testing.T) {
	// Los backups no son por rama: moverlos aca los esconderia del rollback
	// que los necesita.
	ws := repo(t)
	backups := filepath.Join(ws, ".ailoop", "backups")
	if err := os.MkdirAll(backups, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backups, "f.txt"), []byte("respaldo"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, ".ailoop", "state.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := Migrate(ws); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(backups, "f.txt")); err != nil {
		t.Errorf("movio los backups: %v", err)
	}
}

func TestMigrateEsIdempotente(t *testing.T) {
	ws := repo(t)
	if err := os.MkdirAll(filepath.Join(ws, ".ailoop"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, ".ailoop", "state.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := Migrate(ws); err != nil {
		t.Fatal(err)
	}
	moved, err := Migrate(ws)
	if err != nil {
		t.Errorf("la segunda corrida fallo: %v", err)
	}
	if moved {
		t.Error("informo una segunda migracion que no ocurrio")
	}
}

func TestMigrateNoPisaUnEstadoExistente(t *testing.T) {
	// Si hay estado viejo Y estado de la rama, lo peor seria elegir por su
	// cuenta cual sobrevive.
	ws := repo(t)
	if err := os.MkdirAll(AILoopDir(ws), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(AILoopDir(ws), "state.json"), []byte(`{"cual":"nuevo"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, ".ailoop", "state.json"), []byte(`{"cual":"viejo"}`), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := Migrate(ws); err == nil {
		t.Fatal("piso el estado sin avisar")
	}
	data, _ := os.ReadFile(filepath.Join(AILoopDir(ws), "state.json"))
	if !strings.Contains(string(data), "nuevo") {
		t.Errorf("el estado de la rama se perdio: %q", string(data))
	}
}
