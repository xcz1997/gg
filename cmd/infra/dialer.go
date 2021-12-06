package infra

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"github.com/mzz2017/gg/config"
	"github.com/mzz2017/gg/dialer"
	"github.com/sirupsen/logrus"
	"net/url"
	"os"
	"strings"
	"sync"
)

var UnableToConnectErr = fmt.Errorf("unable to connect the proxy node")

func GetDialer(log *logrus.Logger) (d *dialer.Dialer, err error) {
	nodeLink := config.ParamsObj.Node
	if len(nodeLink) > 0 {
		d, err = GetDialerFromLink(nodeLink, config.ParamsObj.TestNode)
		if err != nil {
			return nil, err
		}
		return d, nil
	}
	if config.ParamsObj.Subscription.Link != "" {
		if d, err = GetDialerFromSubscription(log, config.ParamsObj.TestNode); err != nil {
			return nil, err
		}
		return d, nil
	}
	if d, err = GetDialerFromInput(config.ParamsObj.TestNode); err != nil {
		return nil, err
	}
	return d, nil
}

func GetDialerFromLink(nodeLink string, testNode bool) (d *dialer.Dialer, err error) {
	u, err := url.Parse(nodeLink)
	if err != nil {
		return nil, err
	}
	d, err = dialer.NewFromLink(u.Scheme, u.String())
	if err != nil {
		return nil, err
	}
	if testNode {
		if ok, err := d.Test(context.Background()); !ok {
			return nil, fmt.Errorf("%w: %v", UnableToConnectErr, err)
		}
	}
	return d, nil
}

func GetDialerFromInput(testNode bool) (d *dialer.Dialer, err error) {
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Enter the share-link of your proxy: ")
	link, err := reader.ReadString('\n')
	if err != nil {
		return nil, err
	}
	return GetDialerFromLink(strings.TrimSpace(link), testNode)
}

func GetDialerFromSubscription(log *logrus.Logger, testNode bool) (d *dialer.Dialer, err error) {
	if config.ParamsObj.Subscription.Link == "" {
		return nil, fmt.Errorf("subscription link is not set")
	}
	switch config.ParamsObj.Subscription.Select {
	case "manual":
		if config.ParamsObj.Subscription.CacheLastNode {
			d := GetDialerFromSubscriptionLastNodeCache(testNode)
			if d != nil {
				log.Infoln("Use cached node.")
				return d, nil
			}
		}
		// TODO
		log.Fatal("TODO: manual select")
	default:
		log.Warnf("Unexpected select option: %v. Fallback to \"first\".", config.ParamsObj.Subscription.Select)
		fallthrough
	case "first":
		if config.ParamsObj.Subscription.CacheLastNode {
			d := GetDialerFromSubscriptionLastNodeCache(testNode)
			if d != nil {
				log.Infoln("Use cached node.")
				return d, nil
			}
		}
		log.Infoln("Pulling the subscription...")
		dialers, err := getDialersFromSubscription(log, config.ParamsObj.Subscription.Link)
		if err != nil {
			return nil, err
		}
		if testNode {
			log.Infoln("Finding the first available node...")
			if d = firstAvailableDialer(log, dialers); d != nil {
				return d, nil
			}
		} else {
			if len(dialers) > 0 {
				return dialers[0], nil
			}
		}
	}
	return nil, fmt.Errorf("cannot find any available node in your subscription, and you can try again with argument '-vv' to get more information")
}

func firstAvailableDialer(log *logrus.Logger, dialers []*dialer.Dialer) *dialer.Dialer {
	concurrency := make(chan struct{}, 8)
	result := make(chan *dialer.Dialer, cap(concurrency))
	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for _, d := range dialers {
		wg.Add(1)
		go func(d *dialer.Dialer) {
			defer func() { wg.Done() }()
			select {
			case <-ctx.Done():
				return
			case concurrency <- struct{}{}:
				defer func() {
					<-concurrency
				}()
				if ok, err := d.Test(ctx); ok {
					log.Tracef("test pass: %v", d.Name())
					cancel()
					result <- d
				} else if !errors.Is(err, context.Canceled) {
					log.Tracef("test fail: %v: %v", d.Name(), err)
				}
			}
		}(d)
	}
	wg.Wait()
	if len(result) > 0 {
		return <-result
	}
	return nil
}

func GetDialerFromSubscriptionLastNodeCache(testNode bool) (d *dialer.Dialer) {
	if config.ParamsObj.Cache.Subscription.LastNode != "" {
		d, _ := GetDialerFromLink(config.ParamsObj.Cache.Subscription.LastNode, testNode)
		if d != nil {
			return d
		}
	}
	return nil
}