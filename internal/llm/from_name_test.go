package llm

import (
	"strings"
	"testing"
)

// El fallback silencioso ya costo una sesion entera: con GEMINI_API_KEY
// seteada el loop nunca llegaba al local y el sintoma era "el agente no sabe
// usar herramientas". Un proveedor NOMBRADO en el config es una decision del
// usuario: si su credencial falta, el error tiene que nombrar la variable —
// jamas construir otro proveedor en su lugar.
func TestFromNameSinCredencialNombraLaVariable(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")

	for nombre, variable := range map[string]string{
		"claude": "ANTHROPIC_API_KEY",
		"gemini": "GEMINI_API_KEY",
	} {
		c, err := FromName(nombre)
		if err == nil {
			t.Errorf("%s sin credencial construyo %T en vez de fallar", nombre, c)
			continue
		}
		if !strings.Contains(err.Error(), variable) {
			t.Errorf("%s: el error no nombra %s: %v", nombre, variable, err)
		}
	}
}

func TestFromNameDesconocidoFalla(t *testing.T) {
	if c, err := FromName("gpt5"); err == nil {
		t.Fatalf("un proveedor inexistente construyo %T", c)
	} else if !strings.Contains(err.Error(), "gpt5") {
		t.Errorf("el error no nombra el proveedor pedido: %v", err)
	}
}

func TestFromNameConstruyeCadaProveedor(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "k")
	t.Setenv("GEMINI_API_KEY", "k")
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_MODEL", "")

	for _, nombre := range []string{"claude", "gemini", "openai"} {
		c, err := FromName(nombre)
		if err != nil {
			t.Errorf("%s: %v", nombre, err)
			continue
		}
		if c == nil {
			t.Errorf("%s: cliente nil sin error", nombre)
		}
	}
}

// openai es el unico sin credencial obligatoria: su default es Ollama local.
func TestFromNameOpenAIUsaElDefaultLocal(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	if _, err := FromName("openai"); err != nil {
		t.Fatalf("openai sin env deberia construir el local: %v", err)
	}
}
