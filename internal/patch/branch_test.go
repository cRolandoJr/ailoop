package patch

import (
	"os"
	"os/exec"
	"path/filepath"
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

func repoCon(t *testing.T, archivo, contenido string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("sin git")
	}
	ws := t.TempDir()
	git(t, ws, "init", "-q", ".")
	git(t, ws, "config", "user.email", "t@t")
	git(t, ws, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(ws, archivo), []byte(contenido), 0644); err != nil {
		t.Fatal(err)
	}
	git(t, ws, "add", archivo)
	git(t, ws, "commit", "-qm", "base")
	return ws
}

func TestCambiarDeRamaNoEscondeLosBackups(t *testing.T) {
	// El estado se aisla por rama porque una spec pertenece a una linea de
	// trabajo. Los backups NO: son el contenido previo de archivos del
	// working tree, y hay uno solo. Archivarlos bajo el nombre de la rama
	// hacia que "git checkout -b idea" los escondiera, y entonces el rollback
	// no restauraba nada e informaba "nothing to roll back".
	original := "contenido original\n"
	ws := repoCon(t, "codigo.go", original)

	prop := "<<<<\ncodigo.go\n====\ncontenido original\n====\nPARCHEADO\n>>>>\n"
	if err := Apply(ws, prop, NewSeen("codigo.go")); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// El usuario crea una rama, que es justo lo que el aislamiento invita a hacer.
	git(t, ws, "checkout", "-q", "-b", "experimento")

	res, err := Restore(ws)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if len(res.Restored) != 1 {
		t.Errorf("Restored = %v, quiero 1 archivo", res.Restored)
	}

	final, _ := os.ReadFile(filepath.Join(ws, "codigo.go"))
	if string(final) != original {
		t.Errorf("no restauro tras cambiar de rama:\n  got  %q\n  want %q", string(final), original)
	}
}

func TestLosBackupsNoDependenDeLaRama(t *testing.T) {
	ws := repoCon(t, "x.go", "a\n")
	enMain := backupRoot(ws)

	git(t, ws, "checkout", "-q", "-b", "otra")
	enOtra := backupRoot(ws)

	if enMain != enOtra {
		t.Errorf("la ruta de backups cambio con la rama:\n  %s\n  %s", enMain, enOtra)
	}
}
