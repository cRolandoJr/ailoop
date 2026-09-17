package host

import (
	"strings"
	"testing"
)

func TestDiscoverEncuentraLoQueHayYNoInventaLoQueNo(t *testing.T) {
	r := Discover()
	if len(r.Tools) != len(known) {
		t.Fatalf("Tools = %d, quiero %d", len(r.Tools), len(known))
	}
	// sh existe en cualquier unix donde esto corra.
	if !r.Has("sh") {
		t.Error("no encontro sh, que es requerida")
	}
	// una herramienta inexistente no se reporta como presente.
	if r.Has("esta-herramienta-no-existe-12345") {
		t.Error("informo como disponible algo que no consulto")
	}
	for _, tool := range r.Tools {
		if tool.Available() && tool.Path == "" {
			t.Errorf("%s disponible sin ruta", tool.Name)
		}
	}
}

func TestLoQueFaltaSeOrdenaPorImportancia(t *testing.T) {
	r := Report{Tools: []Tool{
		{Name: "c", Need: Optional},
		{Name: "a", Need: Required},
		{Name: "b", Need: Degraded},
	}}
	got := r.Missing()
	if len(got) != 3 {
		t.Fatalf("Missing = %d", len(got))
	}
	if got[0].Name != "a" || got[1].Name != "b" || got[2].Name != "c" {
		t.Errorf("orden = %v", []string{got[0].Name, got[1].Name, got[2].Name})
	}
}

func TestCadaHerramientaDiceQueSePierdeSinElla(t *testing.T) {
	// "no @screen" le dice al usuario que pierde; "screenshots" no.
	for _, tool := range known {
		if strings.TrimSpace(tool.Enables) == "" {
			t.Errorf("%s no dice que se pierde sin ella", tool.Name)
		}
		if strings.TrimSpace(tool.Purpose) == "" {
			t.Errorf("%s no dice para que sirve", tool.Name)
		}
	}
}

func TestElConsejoDeInstalacionNombraPaquetesNoComandos(t *testing.T) {
	// El gestor de paquetes difiere por distribucion, y adivinarlo manda a
	// alguien por el camino equivocado.
	hint := InstallHint([]Tool{{Name: "pdftotext"}, {Name: "grim"}})
	if !strings.Contains(hint, "poppler-utils") {
		t.Errorf("no nombra el paquete real: %q", hint)
	}
	for _, gestor := range []string{"apt ", "pacman ", "dnf ", "brew "} {
		if strings.Contains(hint, gestor) {
			t.Errorf("adivino el gestor de paquetes: %q", hint)
		}
	}
	if InstallHint(nil) != "" {
		t.Error("genero un consejo sin nada que instalar")
	}
}
