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

## Provider predefiniti e sostituzione

Quando una dipendenza richiede `logging.Logger`, il generatore fornisce
`logging.NewDefault()` come fallback. Non servono un provider nell'applicazione,
una cartella `platform/logging` o un binding al logger standard. Ogni initializer
costruisce una sola istanza per il proprio grafo, condivisa dai componenti che
la richiedono. I provider non raggiungibili non vengono emessi né eseguiti.

Per personalizzare soltanto formato e livello, dichiarare un provider scoperto
che restituisce `*logging.SlogLogger`: il provider applicativo sostituisce quello
standard dello stesso tipo.

```go
//pfw:coconut
func NewLogger() *logging.SlogLogger {
    return logging.New(logging.Options{JSON: true, Level: slog.LevelDebug})
}
```

Per usare un altro backend, annotare il costruttore custom e selezionarlo:

```go
pfw.Bind[logging.Logger, *custom.Logger]()
```

Un provider che restituisce direttamente `logging.Logger`, oppure un input
`logging.Logger` dell'initializer, prevale sul fallback senza richiedere binding.
Per i provider concreti continuano a valere le regole di binding esplicito e di
`AutoBind` nei moduli. Più candidati applicativi ambigui restano un errore: il
fallback non sceglie al loro posto. Un binding esplicito a un tipo senza provider
continua a produrre un errore, anche quando esiste il logger standard.

Per un override limitato a un ambiente:

```go
pfw.ForEnv("production",
    pfw.Discover("../infrastructure/customlogging"),
    pfw.Bind[logging.Logger, *custom.Logger](),
),
```

L'ambiente deve appartenere a `pfw.Environments(...)`. Gli altri ambienti usano
il fallback se non hanno una selezione applicativa. Rigenerare il wiring per
l'ambiente scelto dopo ogni modifica dei provider.

## Componente HTTP del framework

Il generatore fornisce anche il provider di `*httpserver.Server`, costruito dal
`*http.Server` applicativo e dal logger scelto dal grafo. Se l'initializer riceve
`pfw.Environment`, il provider aggiunge il nome dell'ambiente al log di readiness.
Un provider o input applicativo dello stesso tipo sostituisce il fallback.

Il componente scrive `server started` dopo il bind riuscito. Handler e server
ricevono lo stesso logger. Il template non contiene un wrapper lifecycle né
costruisce un logger nel main: gli errori di bootstrap sono scritti su stderr,
e il lifecycle coordina il componente fornito dalla DI.

Per costruire il componente direttamente, usare `httpserver.NewWithLogger` o
`httpserver.NewForEnvironment`. `httpserver.New` resta disponibile e usa il logger
standard per compatibilità. Nessun setter globale viene introdotto.

`pfw inspect` mostra `framework_default` nella provenienza dei provider standard
e `fallback: true` nel report JSON. La selezione avviene a build time e il codice
emesso continua a contenere chiamate Go dirette, senza container runtime.
