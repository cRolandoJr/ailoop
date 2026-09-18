package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Medido: con el ejemplo fijo "fs.read: internal/app/loop.go", qwen copio EL
// ARGUMENTO y armo una decision entera sobre un archivo que pertenece a otro
// repo. La plantilla con angulares hacia que copiara las etiquetas; el ejemplo
// fijo hace que copie la ruta. Un archivo real del workspace no tiene ese
// problema: si lo copia, lee algo que existe.
func TestElEjemploUsaUnArchivoRealDelWorkspace(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "cliente.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	p := Protocol(&Registry{Allowed: map[Capability]bool{FSRead: true}}, ws)

	if !strings.Contains(p, "fs.read: cliente.go") {
		t.Errorf("no usa un archivo real del workspace:\n%s", p)
	}
	if strings.Contains(p, "internal/app/loop.go") {
		t.Errorf("sigue usando la ruta fija de otro repo:\n%s", p)
	}
}

// Un workspace sin archivos sueltos en la raiz no puede dejar el ejemplo roto
// ni vacio.
func TestElEjemploSobreviveAUnWorkspaceSinArchivos(t *testing.T) {
	p := Protocol(&Registry{Allowed: map[Capability]bool{FSRead: true}}, t.TempDir())

	if !strings.Contains(p, "<<TOOL>>") || !strings.Contains(p, "fs.read:") {
		t.Errorf("el ejemplo quedo incompleto:\n%s", p)
	}
}

// Y la capacidad del ejemplo sigue saliendo de lo concedido: el agente de
// research no puede enterarse de que existe un filesystem (18.15.7).
func TestElEjemploSigueSaliendoDeLoConcedido(t *testing.T) {
	p := Protocol(&Registry{Allowed: map[Capability]bool{WebFetch: true}}, t.TempDir())
	for _, prohibida := range []string{"fs.read", "fs.grep", "fs.glob", "cmd.run"} {
		if strings.Contains(p, prohibida) {
			t.Errorf("menciona %s sin concederla:\n%s", prohibida, p)
		}
	}
}

// Medido en la corrida: ante un archivo inexistente el agente recibia
// "open /home/rolando/projects/curza-sync/internal/moodle/moodle.go: no such
// file or directory". Dos problemas: le cuenta la ruta absoluta del host, y es
// un errno pelado que no le dice como averiguar que SI hay.
func TestUnArchivoQueNoEstaNoFiltraLaRutaDelHost(t *testing.T) {
	ws := t.TempDir()
	reg := &Registry{Allowed: map[Capability]bool{FSRead: true, FSList: true, FSGlob: true}}

	res := Execute(context.Background(), ws, reg, Request{Cap: FSRead, Arg: "internal/moodle/moodle.go"})

	if res.Err == nil {
		t.Fatalf("leyo algo que no existe: %q", res.Output)
	}
	msg := res.Err.Error()
	if strings.Contains(msg, ws) {
		t.Errorf("filtra la ruta absoluta del host: %q", msg)
	}
	if !strings.Contains(msg, "internal/moodle/moodle.go") {
		t.Errorf("no dice cual archivo: %q", msg)
	}
	if !strings.Contains(msg, "fs.list") && !strings.Contains(msg, "fs.glob") {
		t.Errorf("no le dice como averiguar que hay: %q", msg)
	}
}
