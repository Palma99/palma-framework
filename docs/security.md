# Autenticazione e autorizzazione

`security` offre contratti generici indipendenti da HTTP e dalla forma delle
credenziali. L'applicazione definisce i tipi e il significato dei loro campi:

```go
type Authenticator[C, I any] interface {
    Authenticate(context.Context, C) (I, error)
}
type PrincipalResolver[I, P any] interface {
    Resolve(context.Context, I) (P, error)
}
type Authorizer[P, R any] interface {
    Check(context.Context, P, string, R) error
}
```

Il flusso `C → I → P` separa credenziali, identità verificata e principal:

- SessionCredentials → VerifiedSession → AppPrincipal.
- BearerCredentials → VerifiedJWT → AppPrincipal.
- PasswordCredentials → VerifiedUser → AppPrincipal.

Questi nomi sono tipi dell'applicazione, non tipi imposti dal framework.
Il resolver può usare una query al database, una cache o i dati dell'identità
verificata. Non esistono campi ID/ruoli/permessi obbligatori, né un'identità
universale con claim non tipizzati. Il framework non valida il valore di P:
il resolver deve restituire un principal valido soltanto con errore nil.

## Principal nel context

```go
principal, err := security.Current[AppPrincipal](ctx)
```

Il principal appartiene alla richiesta, mentre i provider DI possono essere
condivisi. Non conservare l'utente corrente in un singleton. Il tipo P usato
in lettura deve essere lo stesso tipo istanziato in scrittura: tipi nominati
diversi e puntatori/valori non sono intercambiabili. Gli alias Go restano lo
stesso tipo. Un tipo errato o un principal assente produce ErrUnauthenticated.

`security.WithPrincipal(ctx, principal)` pubblica un principal da un confine
fidato, anche per chiamate applicative fuori da HTTP. Non verifica credenziali:
non usarlo per accettare direttamente un'identità dichiarata dal client.
Un valore zero pubblicato esplicitamente è presente; il framework non può
dedurne la validità. Mappe, slice e puntatori devono essere trattati come
immutabili dopo la pubblicazione; non vengono copiati dal framework.

`security.WithoutPrincipal[P](ctx)` maschera P conservando cancellazione e
altri valori. Ogni flusso HTTP maschera il principal precedente dello stesso
tipo, per evitare che un'identità precedente sopravviva a un confine anonimo.
Principal di altri tipi rimangono disponibili: proteggere il servizio con
lo stesso P usato dal suo middleware.

## Integrazione net/http

Importare `github.com/palma99/palma-framework/security/http` come `securityhttp`.
L'extractor appartiene al trasporto:

```go
type CredentialExtractor[C any] interface {
    Extract(*http.Request) (C, error)
}
```

Legge cookie, header o altre credenziali e restituisce ErrMissingCredentials
soltanto quando sono assenti. Credenziali presenti ma malformate devono produrre
ErrInvalidCredentials. L'authenticator verifica credenziali, il resolver
costruisce il principal: soltanto Extract può determinare l'assenza opzionale.

```go
mapper := pfwhttp.NewMapper(securityhttp.ErrorRules()...)
middleware, err := securityhttp.NewMiddleware[
    SessionCredentials, VerifiedSession, AppPrincipal,
](extractor, authenticator, resolver, mapper)
if err != nil {
    return nil, err
}
adapter := pfwhttp.NewHandlerAdapter(mapper, logger)
mux.Handle("PATCH /documents/{id}",
    middleware.Required(adapter.Wrap(controller.UpdateTitle)))
mux.Handle("GET /me", middleware.Optional(meHandler))
```

`Required` rifiuta credenziali assenti. `Optional` prosegue senza P soltanto se
l'extractor restituisce ErrMissingCredentials, anche wrappato. Credenziali
invalide, errori dell'authenticator o del resolver interrompono sempre la richiesta.
I risultati parziali accompagnati da un errore non vengono pubblicati né usati.
Il middleware controlla la cancellazione prima del flusso e prima di pubblicare P;
authenticator e resolver devono rispettare il context ricevuto.

`RequiredRequest` e `OptionalRequest` espongono il medesimo flusso ad altri
router: restituiscono una richiesta con il nuovo context, senza modificare
l'originale. I provider devono essere non-nil, inclusi i valori racchiusi nelle
interfacce, e sicuri per uso concorrente. La DI usa costruttori Go ordinari.
Il middleware stdlib usa il logger predefinito per errori server e di scrittura.

## Errori condivisi

`securityhttp.ErrorRules()` restituisce regole registrabili nel mapper HTTP:

| Errore, anche wrappato | Status | Codice pubblico |
| --- | --- | --- |
| ErrMissingCredentials | 401 | unauthenticated |
| ErrInvalidCredentials | 401 | unauthenticated |
| ErrUnauthenticated | 401 | unauthenticated |
| ErrForbidden | 403 | forbidden |

I messaggi pubblici non espongono le cause. Errori infrastrutturali usano il
normale fallback del mapper, 500 generico con l'implementazione standard.
Non convertirli in ErrInvalidCredentials/ErrForbidden. Le regole preservano la
causa per `errors.Is` e logging. Con mapper nil, NewMiddleware usa queste regole
e il fallback standard; un mapper fornito viene rispettato senza aggiunte.
Condividere lo stesso mapper con controller ed Echo per una gestione coerente.
Eventuali challenge HTTP, come WWW-Authenticate per bearer o basic, dipendono
dal meccanismo scelto e vanno aggiunti dall'adapter applicativo.

## Echo

```go
router.HTTPErrorHandler = pfwecho.ErrorHandler(mapper, logger)
group := router.Group("/documents", pfwecho.RequiredSecurity(middleware))
group.PATCH("/:id", controller.UpdateTitle)
router.GET("/me", meHandler, pfwecho.OptionalSecurity(middleware))
```

L'adapter usa il flusso tipizzato del middleware, installa la richiesta nel
context Echo e ripristina quella precedente dopo l'handler, anche in caso di
panic. Gli errori vengono restituiti all'ErrorHandler nativo configurato: nessuna
risposta anticipata o doppia gestione. Non registra middleware globali impliciti.

## Autorizzazione applicativa

```go
principal, err := security.Current[AppPrincipal](ctx)
if err != nil {
    return err
}
document, err := repository.Get(ctx, id)
if err != nil {
    return err
}
if err := authorizer.Check(ctx, principal, "document:update", document); err != nil {
    return err
}
return repository.Save(ctx, document)
```

La policy riceve P e R concreti e applica le regole di ownership, ruoli,
organizzazione o altre condizioni applicative. Azioni non riconosciute devono
essere negate esplicitamente. Il servizio applicativo conserva il controllo
anche quando è chiamato da un altro trasporto. La coerenza tra lettura, verifica
e scrittura, se le condizioni possono cambiare in concorrenza, appartiene al
repository e alla strategia transazionale dell'applicazione.

## Binding DI generici

```go
pfw.Bind[securityhttp.CredentialExtractor[SessionCredentials], *SessionCookieExtractor]()
pfw.Bind[security.Authenticator[SessionCredentials, VerifiedSession], *SessionAuthenticator]()
pfw.Bind[security.PrincipalResolver[VerifiedSession, AppPrincipal], *AppPrincipalResolver]()
pfw.Bind[security.Authorizer[AppPrincipal, Document], *DocumentAuthorizer]()
```

Registrare costruttori non generici con parametri/risultati genericamente
istanziati. Un wrapper applicativo, come NewSecurityMiddleware nell'esempio,
chiama NewMiddleware con i propri tipi. Il generatore verifica i binding con
il type system Go: un authenticator di altri C/I non soddisfa il contratto.
Non occorre un service locator o una DI a runtime.

L'[esempio eseguibile](../examples/security/README.md) include tutte le parti,
repository in memoria e test attraverso il wiring generato. Il modulo non
include un issuer JWT, login locale, password hashing o storage delle sessioni.
I meccanismi concreti sono provider applicativi o adapter futuri; per sessioni
via cookie configurare anche la protezione cross-origin/CSRF dell'applicazione.
