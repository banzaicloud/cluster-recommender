// Package main Cluster Recommender.
//
// This project can be used to recommend instance type groups on different cloud providers consisting of regular and spot/preemptible instances.
// The main goal is to provide and continuously manage a cost-effective but still stable cluster layout that's built up from a diverse set of regular and spot instances.
//
//     Schemes: http, https
//     BasePath: /api/v1
//     Version: 0.0.1
//     License: Apache 2.0 http://www.apache.org/licenses/LICENSE-2.0.html
//     Contact: Banzai Cloud<info@banzaicloud.com>
//
// swagger:meta
package main

import (
	"context"
	"flag"
	"time"

	"github.com/banzaicloud/telescopes/api"
	"github.com/banzaicloud/telescopes/productinfo"
	"github.com/banzaicloud/telescopes/productinfo/ec2"
	"github.com/banzaicloud/telescopes/productinfo/gce"
	"github.com/banzaicloud/telescopes/recommender"
	"github.com/gin-gonic/gin"
	"github.com/patrickmn/go-cache"
	log "github.com/sirupsen/logrus"
)

var (
	rawLevel                   = flag.String("log-level", "info", "log level")
	addr                       = flag.String("listen-address", ":9090", "The address to listen on for HTTP requests.")
	productInfoRenewalInterval = flag.Duration("product-info-renewal-interval", 24*time.Hour, "Duration (in go syntax) between renewing the ec2 product info. Example: 2h30m")
	prometheusAddress          = flag.String("prometheus-address", "", "http address of a Prometheus instance that has AWS spot price metrics via banzaicloud/spot-price-exporter. If empty, the recommender will use current spot prices queried directly from the AWS API.")
	promQuery                  = flag.String("prometheus-query", "avg_over_time(aws_spot_current_price{region=\"%s\", product_description=\"Linux/UNIX\"}[1w])", "advanced configuration: change the query used to query spot price info from Prometheus.")
)

func init() {
	flag.Parse()
	parsedLevel, err := log.ParseLevel(*rawLevel)
	if err != nil {
		log.WithError(err).Warnf("Couldn't parse log level, using default: %s", log.GetLevel())
	} else {
		log.SetLevel(parsedLevel)
		log.Debugf("Set log level to %s", parsedLevel)
	}
}

func main() {

	infoers := make(map[string]productinfo.ProductInfoer, 2)

	ec2Infoer, err := ec2.NewEc2Infoer(ec2.NewPricing(ec2.NewConfig()), *prometheusAddress, *promQuery)
	if err != nil {
		log.Fatalf("could not initialize product info provider: %s", err.Error())
		return
	}
	infoers[recommender.Ec2] = ec2Infoer

	gceInfoer, err := gce.NewGceInfoer()
	if err != nil {
		log.Fatalf("could not initialize product info provider: %s", err.Error())
		return
	}
	infoers[recommender.Gce] = gceInfoer

	c := cache.New(24*time.Hour, 24.*time.Hour)

	productInfo, err := productinfo.NewCachingProductInfo(*productInfoRenewalInterval, c, infoers)
	if err != nil {
		log.Fatal(err)
	}
	go productInfo.Start(context.Background())

	engine, err := recommender.NewEngine(productInfo)
	if err != nil {
		log.Fatal(err)
	}

	routeHandler := api.NewRouteHandler(engine)

	router := gin.Default()
	log.Info("Initialized gin router")
	routeHandler.ConfigureRoutes(router)
	log.Info("Configured routes")

	router.Run(*addr)
}
