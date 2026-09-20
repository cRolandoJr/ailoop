package tools

import (
	"strings"
	"testing"
)

// Medido: qwen escribio **TOOL** en negrita de markdown en lugar de <<TOOL>>,
// dos fases seguidas. Parse corta por "<<TOOL>>", asi que sin el abridor la
// lista de pedidos queda vacia, el loop lo lee como respuesta final y cierra la
// fase. Ni rechazo ni aviso: el pedido se evapora y el agente nunca se entera.
func TestUnBloqueConElAbridorMalNoPasaDesapercibido(t *testing.T) {
	texto := "Veamos el codigo.\n**TOOL**\nfs.read: internal/convert/convert.go\n<<END>>\n"

	problema := MalformedBlock(texto)
	if problema == "" {
		t.Fatal("un bloque con el abridor mal escrito se descarto en silencio")
	}
	if !strings.Contains(problema, "<<TOOL>>") {
		t.Errorf("el aviso no dice cual es el abridor correcto: %q", problema)
	}
}

// Un bloque sin cerrar se descarta igual de callado, y por el mismo motivo.
func TestUnBloqueSinCerrarTampoco(t *testing.T) {
	if MalformedBlock("<<TOOL>>\nfs.read: x.go\n") == "" {
		t.Error("un bloque sin <<END>> se descarto en silencio")
	}
}

// Controles positivos: ni el texto normal ni un bloque bien formado pueden
// dispararlo, o el aviso seria ruido en cada ronda.
func TestTextoSinBloquesNoEsUnProblema(t *testing.T) {
	if p := MalformedBlock("Una respuesta comun, sin herramientas."); p != "" {
		t.Errorf("invento un problema: %q", p)
	}
}

func TestUnBloqueBienFormadoNoEsUnProblema(t *testing.T) {
	if p := MalformedBlock("<<TOOL>>\nfs.read: x.go\n<<END>>"); p != "" {
		t.Errorf("invento un problema sobre un bloque valido: %q", p)
	}
}
