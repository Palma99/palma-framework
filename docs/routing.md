# Controller, routing e middleware

Le rotte sono dichiarate in Go. Il controller riceve i servizi tramite DI e
registra endpoint locali; la composizione HTTP sceglie prefissi, gruppi,
middleware condivisi e server. Gli URL dei template restano `/items` e `/health`.
L'esempio HTTP espone `/users`.

## Echo

Il template Echo genera `internal/item/infrastructure/http/controller.go`:

```go
func (c *Controller) Register(group *echo.Group) {
    group.GET("", c.List)
    group.GET("/:id", c.Get)
    group.POST("", c.Create)
}
```

Il router monta il controller con `items.Register(router.Group("/items"))`.
Per versionare le rotte basta cambiare il montaggio:

```go
api := router.Group("/api/v1")
items.Register(api.Group("/items"))
```

I middleware sono `echo.MiddlewareFunc`. Un componente applicativo può ricevere
tramite costruttore servizi di autenticazione o autorizzazione e restituire
middleware nativi. Per esempio, se `c.auth` è una dipendenza del controller:

```go
group.POST("", c.Create,
    c.auth.Authenticate(),
    c.auth.RequirePermission("items:write"),
)
```

Per proteggere un intero gruppo, la composizione può invece usare:

```go
admin := router.Group("/admin", auth.RequireAdmin())
users.Register(admin.Group("/users"))
```

I middleware globali usano `router.Use(...)` (o `Pre` per la fase precedente
al routing). Echo gestisce l'ordine: middleware globali, di gruppo e di rotta;
per ciascun livello il primo middleware dichiarato avvolge i successivi.
Il codice dopo `next` viene eseguito in ordine inverso. Un middleware può
interrompere la catena senza chiamare l'endpoint. Evitare doppie applicazioni
accidentali dello stesso middleware su più livelli.

Gli handler hanno la firma nativa `func(*echo.Context) error`. Restituiscono
direttamente gli errori dei servizi o di `DecodeJSON`. Il router configura:

```go
router.HTTPErrorHandler = pfwecho.ErrorHandler(mapper, logger)
```

`pfwecho` è `github.com/palma99/palma-framework/transport/echo`.
Il mapper e il logger sono iniettati nel costruttore del router. Gli errori
applicativi usano `pfwhttp.ErrorMapper`; quelli nativi Echo che implementano
`echo.HTTPStatusCoder` mantengono il comportamento nativo, inclusi 404, 405 e
rifiuti dei middleware. Gli errori 5xx vengono registrati senza esporre cause
interne nel payload. Una risposta già iniziata non viene riscritta.

## Standard library

Il controller espone endpoint che restituiscono `error`. `HandlerAdapter.Wrap`
li adatta a normali `http.HandlerFunc`, applicando mapper e logger al confine:

```go
adapter := pfwhttp.NewHandlerAdapter(mapper, logger)
mux.Handle("POST /items", auth.RequireWrite(adapter.Wrap(controller.Create)))
```

Qui `RequireWrite` è un middleware applicativo di tipo
`func(http.Handler) http.Handler`. Le funzioni dei middleware sono codice Go
ordinario; possono avere servizi iniettati e chiudere la richiesta scrivendo
una risposta prima di chiamare `next`.

Il template espone anche un montaggio con prefisso e middleware condivisi:

```go
items.Register(mux, "/api/v1/items")
items.Register(mux, "/admin/items", auth.RequireAdmin())
```

I middleware passati a `Register` vengono applicati a ogni endpoint nell'ordine
dichiarato. Per middleware specifici di un endpoint, modificare la composizione
di quell'endpoint in `Register`, avvolgendo il risultato di `Wrap`.
Per middleware globali, avvolgere il mux assegnato a `http.Server.Handler`.
Non vengono introdotti context o contratti middleware universali.

L'adapter evita di aggiungere una risposta di errore dopo che header o body sono
stati inviati. Per operazioni avanzate sul writer usare `http.ResponseController`,
che può raggiungere il writer originale attraverso `Unwrap`.

## Aggiungere funzionalità

Per un nuovo endpoint, aggiungere il metodo al controller e la registrazione
in `Register`. Per un nuovo controller, aggiungere il suo provider, iniettarlo
nel costruttore del router e scegliere esplicitamente dove montarlo.
La discovery dei provider non espone automaticamente rotte HTTP.

Quando la composizione cresce, suddividerla in provider applicativi come
`PublicAPI` e `AdminAPI`, con i rispettivi controller e middleware. Il router
principale riceve questi moduli invece di ogni singolo controller.

DTO, decoding e traduzione delle richieste restano nel controller. Le regole
applicative e di autorizzazione restano nei servizi; i middleware estraggono
le credenziali HTTP e invocano quei servizi.

## Migrazione

Nei template `Handler`/`NewHandler` diventano `Controller`/`NewController` e
`handler.go` diventa `controller.go`. Gli endpoint sono esportati come `List`,
`Get`, `Create`. I controller Echo non ricevono più mapper e logger: queste
dipendenze passano al router. Quelli stdlib ricevono un `HandlerAdapter`,
costruito dal provider della composizione HTTP. Rigenerare il wiring dopo
aver modificato i costruttori.
