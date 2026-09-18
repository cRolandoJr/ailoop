package state

import (
	"fmt"
	"sync"
	"testing"
)

// Reproduce lo que paso a mano: `ailoop cost` contra una sesion que estaba
// guardando dio "unexpected end of JSON input". El archivo no quedo corrupto
// -era una lectura de un archivo a medias- porque os.WriteFile TRUNCA y
// despues escribe. En esa ventana el estado no existe.
//
// La misma ventana pierde el work item entero si el proceso muere ahi.
func TestGuardarYLeerALaVezNuncaMuestraUnArchivoAMedias(t *testing.T) {
	dir := t.TempDir()
	s := NewState("tarea")
	if err := Save(dir, s); err != nil {
		t.Fatal(err)
	}

	parar := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-parar:
				return
			default:
			}
			copia := *s
			copia.TaskDescription = fmt.Sprintf("tarea %d con texto suficiente para que el archivo pese", i)
			_ = Save(dir, &copia)
		}
	}()

	var rotas int
	var ultimo error
	for i := 0; i < 500; i++ {
		if _, err := Load(dir); err != nil {
			rotas++
			ultimo = err
		}
	}
	close(parar)
	wg.Wait()

	if rotas > 0 {
		t.Fatalf("%d de 500 lecturas fallaron, la ultima: %v", rotas, ultimo)
	}
}
