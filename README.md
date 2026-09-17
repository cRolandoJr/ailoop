# ailoop

Motor de workflow para desarrollo asistido por LLM. Implementa el protocolo
**AI Loop**: un trabajo avanza por fases, cada artefacto se aprueba
explícitamente, y **el cierre lo otorga el proyecto, no el modelo**.

Funciona con cualquier proveedor —Claude, Gemini, cualquier endpoint compatible
con OpenAI, Ollama local— sin cambiar el flujo.

## La idea en una línea

> El LLM propone; el workflow valida y autoriza.

Un agente que termina su turno no tiene con eso permiso para avanzar. Cada
transición tiene guardas, y `DONE` se concede sólo después de correr los
comandos que el propio proyecto declaró.

## Uso

```sh
ailoop start "descripción de la tarea"   # crea el contexto y detecta el stack
ailoop next [--files a.go,b.go]          # corre el agente de la fase actual
ailoop next --inline                     # ...pegando esos archivos enteros en el prompt
ailoop next --critic                     # ...con revisión adversarial
ailoop status                            # fase actual y estado de aprobación
ailoop history                           # revisiones archivadas de cada artefacto
ailoop decide "qué" "por qué"            # registra una decisión aprobada
ailoop decisions                         # lista el Decision Record
ailoop verify                            # corre los checks del proyecto
ailoop capabilities                      # qué puede el modelo, y qué permite cada fase
ailoop mcp [--schemas]                   # herramientas de los servidores MCP
ailoop cost                              # gasto de tokens por fase
ailoop undo                              # vuelve atrás y revierte los parches
```

### Configuración del modelo

Se elige por variable de entorno, en este orden:

| Variable | Efecto |
|---|---|
| `ANTHROPIC_API_KEY` | usa Claude (`ANTHROPIC_MODEL`, por defecto `claude-opus-5`) |
| `GEMINI_API_KEY` | usa Gemini (`GEMINI_MODEL`, por defecto `gemini-2.5-pro`) |
| `OPENAI_BASE_URL` | endpoint compatible OpenAI (por defecto `http://localhost:11434/v1`, Ollama) |
| `OPENAI_API_KEY`, `OPENAI_MODEL` | credencial y modelo de ese endpoint |

## Las fases

```
DISCOVERY -> DESIGN -> PLAN -> IMPLEMENTATION -> VERIFICATION -> DONE
```

Volver atrás con `undo` no destruye lo escrito: la revisión se archiva y se
puede recuperar con `ailoop history`.

## Los tres niveles de control

El valor del motor no está en los prompts sino en lo que **no depende** de que
el modelo se porte bien.

| Nivel | Mecanismo | Dónde vive |
|---|---|---|
| **Determinista** | los comandos del proyecto deciden si se cierra | `internal/verify` |
| **Estructural** | guardas de transición, parseo de parches, validación de rutas, chequeo de citas | `internal/state/guards.go`, `internal/patch`, `internal/agents` |
| **Semántico** | agente crítico adversarial | `internal/agents/critic.go` |

Un agente no puede concederse una excepción a ninguno de los dos primeros.

## Verificación: qué significa "listo"

`ailoop start` escribe `.ailoop/config.json` con los comandos que definen
"verificado" para este proyecto:

```json
{
  "verify": [
    {"name": "build", "cmd": "go build ./..."},
    {"name": "test",  "cmd": "go test ./... -count=1"}
  ],
  "timeout_seconds": 300
}
```

Si el stack no se reconoce, la lista queda **vacía** y el loop no puede llegar
a `DONE` hasta que la completes. Una lista vacía nunca se lee como "todo bien":
que no haya corrido nada no es que haya pasado todo.

## Herramientas del agente

El agente puede mirar el proyecto real en lugar de imaginarlo:

```
<<TOOL>>
fs.grep: func Normalizar -- **/*.go
<<END>>
```

| Capacidad | Qué hace |
|---|---|
| `fs.read` | leer un archivo del workspace |
| `fs.list` | listar un directorio |
| `fs.glob` | buscar archivos por patrón (`**` cruza directorios) |
| `fs.grep` | buscar en contenidos por expresión regular, con `-- <glob>` opcional |
| `fs.read_image` | adjuntar una imagen, **sólo si el modelo ve** |
| `mcp.describe` / `mcp.call` | herramientas de servidores externos |
| `cmd.run` | correr un check declarado, **sólo en VERIFICATION** |

El protocolo es texto plano a propósito: el *function calling* nativo varía
entre proveedores y algunos modelos locales no lo tienen.

**Capacidad, permiso y autoridad son tres cosas distintas.** Una capacidad
necesita dos compuertas: que la fase la permita **y** que el modelo la tenga.
`ailoop capabilities` muestra las dos.

Sólo el verificador puede correr los checks: un implementador capaz de correr y
arreglar sus propios tests es su propio verificador.

## Servidores MCP

Cualquier servidor del Model Context Protocol se convierte en capacidades del
loop, sin reimplementarlo acá:

```json
"mcp": [
  {"name": "codegraph", "command": "cgc-nix", "args": ["mcp", "start"]}
]
```

Opcionalmente `"tools": ["find_code", "analyze_code_relationships"]` por
servidor, para exponer sólo lo que usás.

**Las herramientas se cargan diferidas.** El prompt lleva un catálogo de una
línea por herramienta; el agente pide `mcp.describe` sólo para la que va a
usar. Medido contra CodeGraph (25 herramientas): **465 tokens contra 1.660, un
72% menos en cada request**. `ailoop mcp` muestra la comparación.

Un servidor que no arranca se reporta y sus herramientas **no se ofrecen**:
planificar alrededor de una herramienta muerta es peor que no tenerla.

## Gasto de tokens

`ailoop cost` da el desglose por fase, con tasa de acierto de caché. Los tres
adaptadores leen el uso real que reporta su API; cuando un servidor local no
reporta nada, se estima y **se marca como estimado** — y esa marca es pegajosa:
un total que mezcla medido con estimado es un estimado.

Cuatro medidas, todas agnósticas del proveedor salvo donde se indica:

| Medida | Efecto |
|---|---|
| **Prefijo estable** | el system prompt no lleva nada volátil. Claude necesita el marcador explícito, los endpoints OpenAI lo hacen solos; la forma correcta del prompt es la misma |
| **Catálogo MCP diferido** | −72% del costo de las definiciones de herramientas |
| **`--files` por referencia** | nombra los archivos; el agente lee los que necesita. `--inline` conserva el volcado |
| **Observation masking** | elide observaciones viejas del tool loop. Medido: **−52% a 6 rondas, −68% a 10** |

El masking **no se activa** por debajo de 12k tokens: reescribir mensajes
viejos invalida el prefijo cacheado, y eso costaría más de lo que ahorra en una
conversación chica. Nunca elide una observación que contiene un error —
esconderlo rompe el ciclo que lo está diagnosticando.

## Investigación en internet

Un agente que escribe código y además navega puede poner tu código en una URL. Por eso
**no es el mismo agente**:

| | Agente principal | Agente investigador |
|---|---|---|
| Workspace, código, decisiones | ✅ | ❌ |
| `fs.read`, `fs.grep`, `fs.glob` | ✅ | ❌ |
| **Red** | ❌ **en toda fase** | ✅ |
| Recibe | la tarea completa | **sólo una pregunta, máximo 500 caracteres** |

El principal delega con `research.ask`; nunca toca la red él mismo. El investigador no puede
filtrar lo que nunca tuvo — es una propiedad estructural, no una apuesta sobre el
comportamiento del modelo.

**Busca donde sea.** No hay allowlist de destinos: restringirlos no defiende del canal real
—lo que sale es la pregunta— y volvería inútil la investigación, porque no se sabe de
antemano dónde está la respuesta.

**Salvo la red local**, que se bloquea siempre y no es configurable: `localhost`, IPs
privadas, link-local y `.internal`. El investigador no tiene nada del proyecto, pero corre
dentro de tu perímetro; alcanzar tu router o el endpoint de metadata de un cloud no es
exfiltración, es SSRF. Los redirects se vuelven a chequear, por la misma razón.

Todo lo que trae llega envuelto en `UNTRUSTED_CONTENT` con la regla al lado del contenido:
es dato de terceros, nunca instrucciones. La inyección de prompt es el caso esperado.

```json
"web": {"enabled": true}
```

`enabled` es explícito y `ailoop start` lo escribe en el config para que lo veas y puedas
apagarlo. Opcionalmente `"allowed"` restringe destinos y `"blocked"` los excluye.

**Lo que queda abierto:** la pregunta la formula el agente principal, que sí ve el código.
Ese canal no se cierra sin volver inútil la función; se mantiene **angosto** (500
caracteres), **visible** (se muestra como cualquier otra herramienta) y **registrado**.

## Referencias en el texto

Cualquier cosa que escribas —la tarea, el motivo de un rechazo— acepta referencias:

```
ailoop start "arreglá el alineado, mirá @captura.png y @src/boton.tsx"
```

| Referencia | Qué adjunta | Necesita |
|---|---|---|
| `@main.go` | el contenido del archivo | — |
| `@~/Descargas/spec.pdf` | el texto del PDF | `pdftotext` |
| `@captura.png` | la imagen | un modelo con visión |
| `@screen` / `@screen:select` | la pantalla, o una región | `grim` / `slurp` |
| `@clipboard` | lo que tengas copiado, texto o imagen | `wl-paste` |

`ailoop doctor` te dice cuáles de esas herramientas tenés y qué perdés por las que falten.

### Dos reglas que hacen que esto sea seguro

**Las referencias son tuyas, no del agente.** Por eso pueden salir del workspace:
escribir `@~/Descargas/spec.pdf` **es** la autorización, dada caso por caso. El
agente, con `fs.read` y `fs.grep`, sigue confinado al workspace — ahí nadie
autorizó nada.

**`@screen` nunca es una capacidad del agente.** Sólo ocurre porque vos lo
escribiste. Un agente que pudiera capturar la pantalla cuando quisiera vería tu
gestor de contraseñas, tu correo, lo que tengas abierto.

### Lo volátil se congela

Si rechazás una propuesta con *"esto está mal, mirá @screen"*, la captura se hace
**en ese momento** y se guarda junto al estado del trabajo. El agente lee ese motivo
en la corrida siguiente, cuando la pantalla ya muestra otra cosa; sin congelarla,
fotografiaría cualquier cosa.

## Portabilidad

El núcleo es Go y no depende de nada: la máquina de estados, las guardas, el ledger,
el presupuesto y el parcheo se comportan igual en todas partes. Lo que varía es el
entorno, y esas capacidades se **descubren**, no se asumen.

```sh
nix develop    # desarrollar: las herramientas en el PATH
nix build      # el binario, con sus dependencias colgadas
nix run github:cRolandoJr/ailoop
```

La diferencia importa: un devShell resuelve el PATH de quien lo abre; el paquete usa
`wrapProgram`, así que el binario lleva `poppler-utils`, `grim`, `slurp` y
`wl-clipboard` sin que estén instalados en la máquina.

Sin Nix también funciona: las capacidades cuya herramienta falte simplemente no se
ofrecen, y `ailoop doctor` dice cuáles son.

## Seguridad de los parches

- Las rutas se validan contra el workspace: se rechazan absolutas y las que
  escapan con `..`.
- Si **algún** bloque del parche es inválido, no se aplica **ninguno**.
- Un bloque de búsqueda ambiguo (varias coincidencias) se rechaza en lugar de
  parchear "la primera".
- Cada archivo se respalda preservando su ruta antes de tocarlo, y `undo`
  restaura de verdad, informando qué restauró y qué no pudo.
- `cmd.run` ejecuta **únicamente** los comandos declarados en el config: no hay
  shell arbitrario en ningún camino.

## Arquitectura

```
main.go                 CLI y orquestación
internal/state          fases, guardas, historial, Decision Record, ledger de gasto
internal/verify         ejecuta los comandos del proyecto
internal/config         qué significa "verificado" acá, y qué servidores MCP hay
internal/agents         agentes por fase, crítico, permisos, tool loop, masking, citas
internal/tools          capacidades y su ejecución acotada
internal/mcp            cliente del Model Context Protocol
internal/patch          parches con respaldo y rollback
internal/llm            puerto de LLM + adaptadores (Claude, Gemini, OpenAI)
internal/ui             aprobación humana
```

`internal/llm.Client` es un puerto de dos métodos: agregar un proveedor nuevo
no toca nada del resto.

## Estado

MVP funcional, con tests. Pendiente:

- el adaptador de Claude está probado en su lógica pero todavía no llamó a la
  API real;
- el adaptador OpenAI-compatible no envía imágenes (por eso declara
  `Vision: unknown`, que es fail-closed y nunca se le ofrecen);
- Gemini tiene *context caching* explícito que este adaptador no usa.

## Licencia

MIT — ver [LICENSE](LICENSE).
