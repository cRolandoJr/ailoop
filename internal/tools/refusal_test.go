package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/cRolandoJr/ailoop/internal/web"
)

// Medido contra qwen2.5-coder: leyo la plantilla "<capability>: <argument>"
// como etiquetas literales y emitio "capability: fs.glob". Es una lectura
// razonable de un texto con angulares.
func TestParseAceptaLaFormaEtiquetada(t *testing.T) {
	reqs := Parse("bla\n<<TOOL>>\ncapability: fs.glob\nargument: internal/**/*.go\n<<END>>\n")

	if len(reqs) != 1 {
		t.Fatalf("hay %d pedidos, quiero 1: %+v", len(reqs), reqs)
	}
	if reqs[0].Cap != FSGlob {
		t.Errorf("Cap = %q, quiero %q", reqs[0].Cap, FSGlob)
	}
	if reqs[0].Arg != "internal/**/*.go" {
		t.Errorf("Arg = %q, quiero el patron solo", reqs[0].Arg)
	}
}

// Control positivo: la forma normal, que es la que el protocolo pide, no se
// puede romper al aceptar la otra.
func TestParseSigueAceptandoLaFormaNormal(t *testing.T) {
	reqs := Parse("<<TOOL>>\nfs.grep: func Normalizar -- **/*.go\n<<END>>")

	if len(reqs) != 1 {
		t.Fatalf("hay %d pedidos, quiero 1", len(reqs))
	}
	if reqs[0].Cap != FSGrep || reqs[0].Arg != "func Normalizar -- **/*.go" {
		t.Errorf("got %+v", reqs[0])
	}
}

// El defecto que costo 6 rondas: un nombre que no existe se reportaba como
// problema de PERMISOS, asi que el agente cambiaba de herramienta en vez de
// corregir la sintaxis, y volvia a fallar igual.
func TestUnNombreInexistenteNoSeReportaComoFaltaDePermiso(t *testing.T) {
	reg := &Registry{Allowed: map[Capability]bool{FSRead: true, FSGlob: true}}
	res := Execute(context.Background(), t.TempDir(), reg,
		Request{Cap: "capability", Arg: "fs.glob"})

	if strings.Contains(res.Output, "not permitted in this phase") {
		t.Errorf("le echa la culpa a los permisos: %q", res.Output)
	}
	if !strings.Contains(res.Output, "fs.glob") {
		t.Errorf("no le dice que nombres hay: %q", res.Output)
	}
}

// Y una capacidad que SI existe pero no toca en esta fase tiene que seguir
// diciendo lo que decia: ahi el problema si es de permisos.
func TestUnaCapacidadRealFueraDeFaseSigueSiendoUnPermiso(t *testing.T) {
	reg := &Registry{Allowed: map[Capability]bool{FSRead: true}}
	res := Execute(context.Background(), t.TempDir(), reg,
		Request{Cap: CmdRun, Arg: "test"})

	if !strings.Contains(res.Output, "not permitted in this phase") {
		t.Errorf("no dice que es de permisos: %q", res.Output)
	}
}

// La plantilla con angulares es lo que indujo el error. Un ejemplo concreto
// no se puede leer como etiquetas.
func TestElProtocoloNoEnsenaLaPlantillaConAngulares(t *testing.T) {
	p := Protocol(&Registry{Allowed: map[Capability]bool{FSRead: true}}, t.TempDir())
	if strings.Contains(p, "<capability>") {
		t.Errorf("el protocolo sigue enseniando marcadores angulares:\n%s", p)
	}
}

// El ejemplo del protocolo tiene que salir de lo CONCEDIDO. Hardcodearlo a
// fs.read se lo mostraba hasta al agente de research, que por diseño no debe
// enterarse de que existe el filesystem (AI_LOOP 18.15.7).
func TestElEjemploDelProtocoloUsaUnaCapacidadConcedida(t *testing.T) {
	reg := &Registry{Allowed: map[Capability]bool{WebFetch: true}, Web: &web.Fetcher{}}
	p := Protocol(reg, t.TempDir())

	for _, prohibida := range []string{"fs.read", "fs.grep", "fs.glob", "cmd.run"} {
		if strings.Contains(p, prohibida) {
			t.Errorf("el protocolo menciona %s sin concederla:\n%s", prohibida, p)
		}
	}
	if !strings.Contains(p, "web.fetch") {
		t.Errorf("no usa la que si concede:\n%s", p)
	}
}
