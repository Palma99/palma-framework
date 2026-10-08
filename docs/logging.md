# Logger sostituibile

Il package `github.com/palma99/palma-framework/logging` espone un contratto
indipendente dall'implementazione e un logger standard basato su `log/slog`:

```go
type Logger interface {
    Debug(context.Context, string, ...any)
    Info(context.Context, string, ...any)
    Warn(context.Context, string, ...any)
    Error(context.Context, string, ...any)
    With(...any) Logger
}
```

I campi vengono passati come coppie chiave/valore. `With` aggiunge campi a un
logger derivato senza modificare quello originale. Le implementazioni devono
essere sicure per l'uso concorrente. Il logger scrive eventi: il controllo del
processo, incluso l'exit code, resta all'applicazione.

```go
logger := logging.New(logging.Options{})
requestLogger := logger.With("request_id", "abc123")
requestLogger.Info(ctx, "user created", "user_id", user.ID)
```

Con opzioni vuote, `SlogLogger` scrive su stderr in formato testo da livello info.
Non modifica il logger globale di `slog`. Per configurarlo:

```go
logger := logging.New(logging.Options{
    Output: os.Stdout,
    Level: slog.LevelDebug,
    JSON: true,
})
```

L'implementazione standard supporta anche i campi `slog.Attr`. Ogni logger riceve
il contesto della chiamata: per i log HTTP viene passato quello della richiesta.

## Registrazione e sostituzione

Il template API include `internal/platform/logging/logger.go`:

```go
//pfw:coconut
func NewLogger() *logging.SlogLogger {
    return logging.New(logging.Options{})
}
```

La composizione scopre il provider attraverso `pfw.Discover("../...")` e dichiara:

```go
pfw.Bind[logging.Logger, *logging.SlogLogger]()
```

Handler e componente lifecycle ricevono `logging.Logger` nei costruttori e
condividono la stessa istanza creata dal grafo. Per sostituire l'implementazione:

1. Creare un tipo che implementi `logging.Logger`, anche come adapter di un logger esistente.
2. Annotare il suo costruttore con `//pfw:coconut` in un package scoperto.
3. Sostituire il binding, ad esempio con `pfw.Bind[logging.Logger, *custom.Logger]()`.
4. Rigenerare il wiring per l'ambiente scelto.

Il provider standard può rimanere dichiarato: quando non è raggiungibile nel
grafo selezionato, il codice generato non lo costruisce.

Per scegliere un logger diverso solo in un ambiente, mantenere il binding comune
e aggiungere un binding specifico:

```go
pfw.Bind[logging.Logger, *logging.SlogLogger](),
pfw.ForEnv("production",
    pfw.Bind[logging.Logger, *custom.Logger](),
),
```

L'ambiente deve appartenere a `pfw.Environments(...)` e il costruttore custom
deve essere registrato o scoperto. Il framework non interpreta il nome del logger
o dell'ambiente per scegliere implementazioni.

## Log degli skeleton API

Entrambi i router, stdlib ed Echo, usano il logger iniettato per readiness ed
errori HTTP. Il server scrive `server started` con `address` ed `environment`
dopo il bind riuscito. Gli errori interni sono registrati dal logger e mappati
in risposte pubbliche attraverso il mapper HTTP.

Il main usa inizialmente un logger standard per segnalare errori prima che la
composizione sia completata. Dopo l'inizializzazione usa il logger del grafo anche
per gli errori restituiti dal lifecycle. Non esiste un setter globale: la scelta
del logger appartiene al composition root dell'applicazione.
