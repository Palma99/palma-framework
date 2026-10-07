# Funzionalità HTTP

Il package `github.com/palma99/palma-framework/http`, importato come `pfwhttp`,
offre tipi di risposta/errore, JSON e mapping degli errori applicativi.
Dipende dalla standard library e può essere utilizzato con Echo, Chi o handler
`net/http`: non impone un context del framework né un router.

## Risposte

```go
return pfwhttp.OK(userDTO).Write(w)
return pfwhttp.Created("/users/2", userDTO).Write(w)
return pfwhttp.NoContent().Write(w)
```

`Response[T]` contiene `Status`, `Headers` e `Body`. `JSON(status, body)` permette
altri status. La serializzazione JSON avviene prima della scrittura degli header:
un errore di encoding non lascia una risposta di successo parzialmente scritta.
Gli errori di scrittura vengono restituiti al chiamante. Le risposte 204/304
non serializzano un body. Il payload è serializzato direttamente, senza envelope
obbligatori, per lasciare all'applicazione la scelta dei DTO.

In Echo `w` può essere `ctx.Response()`. L'esempio HTTP usa questo percorso.

## Deserializzazione JSON

```go
input, err := pfwhttp.DecodeJSON[CreateUserRequest](r, pfwhttp.DecodeOptions{
    MaxBodyBytes: 1 << 20,
})
```

Il decoder richiede `application/json` (sono ammessi parametri come charset)
e un singolo valore JSON. Il body viene letto al massimo fino al limite più
un byte; il valore predefinito è 1 MiB. Non chiude il body, che resta gestito
dal server/chiamante.

- Media type errato: 415, `unsupported_media_type`.
- Body oltre il limite: 413, `payload_too_large`.
- JSON vuoto, malformato, con valori aggiuntivi, tipi o campi errati: 400,
  `invalid_request`.
- I campi sconosciuti sono rifiutati di default; `AllowUnknownFields: true`
  permette di ignorarli.

Questi errori sono `*pfwhttp.Error` con messaggio pubblico e causa interna quando
disponibile. La validazione semantica rimane nei servizi/dominio dell'applicazione.

## Errori e mapping

`Error` contiene status HTTP, codice pubblico, messaggio e campi opzionali.
Soltanto `code`, `message` e `fields` vengono serializzati. `WithCause` preserva
la causa attraverso `Unwrap`; `WithFields` copia i dettagli dei campi.

```go
mapper := pfwhttp.NewMapper(
    pfwhttp.As(func(err *domain.ValidationError) *pfwhttp.Error {
        return pfwhttp.NewError(422, "validation_failed", "invalid input").
            WithFields(map[string]string{err.Field: err.Message})
    }),
    pfwhttp.Is(domain.ErrNotFound,
        pfwhttp.NewError(404, "not_found", "user not found")),
)
```

`Is` usa `errors.Is`, `As` usa `errors.As`, quindi funzionano anche con errori
wrappati o aggregati. Il mapper conserva i messaggi degli errori HTTP già tipizzati;
per gli errori applicativi usa la prima regola corrispondente. Il fallback è un
500 generico `internal_error`, senza serializzare messaggi o cause interne.
`Map(nil)` restituisce nil. Le regole vengono configurate alla costruzione e
devono essere sicure per uso concorrente; il mapper copia i campi di ogni risultato.

```go
if err != nil {
    return mapper.Write(w, err)
}
```

Oppure `mapper.Map(err)` restituisce un errore HTTP utilizzabile dalle API native
del router. La causa resta accessibile per logging tramite `errors.Is`/`errors.As`.
Il dominio non deve conoscere status HTTP, importare questo package o implementare
interfacce del framework: il mapping appartiene all'adapter di trasporto.

I consumer possono dipendere dall'interfaccia `pfwhttp.ErrorMapper`:

```go
type ErrorMapper interface {
    Map(error) *Error
    Write(http.ResponseWriter, error) error
}
```

`*pfwhttp.Mapper` è l'implementazione fornita dal framework; l'applicazione può
fornirne un'altra che soddisfa questo contratto.

## Integrazione DI

Nell'esempio `internal/user/adapter/http`, `NewErrorMapper` è un costruttore
annotato `//pfw:coconut`. Il controller riceve `pfwhttp.ErrorMapper` nel costruttore.
Il modulo `Users` abilita `AutoBind()`:
il generatore seleziona `*pfwhttp.Mapper` perché è l'unica implementazione
registrata dell'interfaccia. Se ne vengono registrate più di una occorre
`pfw.Implementation` o `pfw.Bind`; senza `AutoBind` il binding va dichiarato.
Gli errori di routing o middleware restano gestibili con gli strumenti nativi
della libreria HTTP scelta.
