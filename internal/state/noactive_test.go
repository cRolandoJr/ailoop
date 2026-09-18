package state

import (
	"errors"
	"strings"
	"testing"
)

// El dominio no sabe quien lo llama. Un error que dice "corre ailoop start"
// obliga a todas las superficies a dar el consejo de la terminal, incluida la
// sesion, donde ese consejo es falso.
func TestCargarSinLoopDaUnCentinelaSinConsejoDeShell(t *testing.T) {
	_, err := Load(t.TempDir())
	if err == nil {
		t.Fatal("cargar un directorio vacio no dio error")
	}
	if !errors.Is(err, ErrNoActiveLoop) {
		t.Errorf("err = %v, quiero que sea ErrNoActiveLoop", err)
	}
	if strings.Contains(err.Error(), "ailoop start") {
		t.Errorf("err = %q: el dominio esta prescribiendo un comando de shell", err)
	}
}
