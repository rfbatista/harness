package app

import (
	"context"
	"fmt"
	"log/slog"
	"reflect"
	"time"

	"go.uber.org/fx"
	"gorm.io/gorm"

	"operators-mcp/internal/adapter/out/notifylog"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/notifications"
	"operators-mcp/internal/application/taskchannel"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// NotificationsModule wires the person's inbox: the service over its table,
// its bus subscriptions, the review requests the architect channel announces,
// the delivery providers, the delivery workers and the hourly prune.
var NotificationsModule = fx.Module("notifications",
	// Providers: each is an adapter (see "Contract: Notifications ↔ delivery
	// providers") plus one line here.
	fx.Provide(
		deliveryProvider(func() ports.NotificationDelivery { return notifylog.New(slog.Default()) },
			notifications.Defaults{Enabled: true, Level: domain.NotificationInfo}),
	),
	fx.Provide(newNotificationsService),
	fx.Invoke(registerNotifications),
)

// providerGroup is the fx value group the delivery providers are collected
// in.
const providerGroup = `group:"notification_providers"`

// deliveryProvider puts a delivery provider into the providers group: ctor
// is an fx constructor (any parameters, from the graph) returning a
// ports.NotificationDelivery, and optionally an error; defaults apply while
// its settings are unset.
func deliveryProvider(ctor any, defaults notifications.Defaults) any {
	v := reflect.ValueOf(ctor)
	t := v.Type()
	delivery := reflect.TypeFor[ports.NotificationDelivery]()
	errType := reflect.TypeFor[error]()
	if t.Kind() != reflect.Func || t.NumOut() < 1 || t.NumOut() > 2 || !t.Out(0).Implements(delivery) ||
		(t.NumOut() == 2 && t.Out(1) != errType) {
		panic(fmt.Sprintf("deliveryProvider: %s is not a constructor of a ports.NotificationDelivery", t))
	}
	in := make([]reflect.Type, t.NumIn())
	for i := range in {
		in[i] = t.In(i)
	}
	fnType := reflect.FuncOf(in, []reflect.Type{reflect.TypeFor[notifications.Provider](), errType}, t.IsVariadic())
	fn := reflect.MakeFunc(fnType, func(args []reflect.Value) []reflect.Value {
		var out []reflect.Value
		if t.IsVariadic() {
			out = v.CallSlice(args)
		} else {
			out = v.Call(args)
		}
		if len(out) == 2 && !out[1].IsNil() {
			return []reflect.Value{reflect.Zero(fnType.Out(0)), out[1]}
		}
		d, _ := out[0].Interface().(ports.NotificationDelivery)
		if d == nil {
			return []reflect.Value{reflect.Zero(fnType.Out(0)), reflect.ValueOf(fmt.Errorf("deliveryProvider: %s returned no provider", t)).Convert(errType)}
		}
		return []reflect.Value{reflect.ValueOf(notifications.Provider{Delivery: d, Defaults: defaults}), reflect.Zero(errType)}
	})
	return fx.Annotate(fn.Interface(), fx.ResultTags(providerGroup))
}

type notificationsParams struct {
	fx.In

	DB        *gorm.DB
	Projects  ports.ProjectRepository
	Tickets   ports.TicketRepository
	Sessions  ports.SessionRepository
	Cat       catalog.Catalog
	Channel   *taskchannel.Service
	Providers []notifications.Provider `group:"notification_providers"`
}

// newNotificationsService builds the inbox, subscribes it to the bus, and
// has the architect channel announce review requests there. Names are read
// from the stores directly, never through the services, so a handler running
// inside a publisher's call cannot take a lock the publisher holds.
func newNotificationsService(p notificationsParams) (*notifications.Service, error) {
	svc, err := notifications.New(sqlite.NewNotificationRepository(p.DB),
		notifications.Readers{Projects: p.Projects, Tickets: p.Tickets, Sessions: p.Sessions},
		notifications.Options{Providers: p.Providers, Settings: p.Cat.Settings})
	if err != nil {
		return nil, err
	}
	svc.Subscribe(p.Cat.Bus)
	p.Channel.Events = p.Cat.Bus
	return svc, nil
}

// pruneEvery is how often old read or resolved notifications are pruned.
const pruneEvery = time.Hour

// deliveryDrain bounds how long stopping waits for the providers' queues.
const deliveryDrain = 5 * time.Second

// registerNotifications runs the delivery workers and the prune loop from
// start to stop.
func registerNotifications(lc fx.Lifecycle, svc *notifications.Service) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			svc.StartDelivery()
			go func() {
				defer close(done)
				t := time.NewTicker(pruneEvery)
				defer t.Stop()
				for {
					if n, err := svc.Prune(ctx); err != nil {
						slog.Warn("prune notifications", "err", err)
					} else if n > 0 {
						slog.Info("pruned notifications", "count", n)
					}
					select {
					case <-ctx.Done():
						return
					case <-t.C:
					}
				}
			}()
			return nil
		},
		OnStop: func(stop context.Context) error {
			cancel()
			<-done
			drain, cancelDrain := context.WithTimeout(stop, deliveryDrain)
			defer cancelDrain()
			svc.StopDelivery(drain)
			return nil
		},
	})
}
