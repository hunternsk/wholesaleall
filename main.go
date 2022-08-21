package main

import (
	"context"
	"encoding/json"
	"flag"
	"github.com/adshao/go-binance/v2"
	"github.com/davecgh/go-spew/spew"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type configType struct {
	APIKey    string            `json:"api_key"`
	SecretKey string            `json:"secret_key"`
	SellAll   map[string]string `json:"sell_all"`
}

type tradeJob struct {
	From   string
	To     string
	Amount float64
}

var (
	configFile = flag.String("c", "config.json", "config filename")
	debug      = flag.Bool("debug", false, "debug")

	config = configType{
		APIKey:    "",
		SecretKey: "",
		SellAll:   map[string]string{"eth": "usdt"},
	}
)

func readConfig(cfg *configType) error {
	configFileName := "config.json"
	if len(os.Args) > 1 {
		configFileName = *configFile
	}
	configFileName, _ = filepath.Abs(configFileName)
	log.Printf("Loading config: %v", configFileName)

	configFile, err := os.Open(configFileName)
	if err != nil {
		log.Println("File error: ", err.Error())
		return err
	}
	defer configFile.Close()
	jsonParser := json.NewDecoder(configFile)
	if err := jsonParser.Decode(&cfg); err != nil {
		log.Println("Config error: ", err.Error())
		return err
	}
	return nil
}

func main() {
	log.SetFlags(log.Lshortfile)
	flag.Parse()
	err := readConfig(&config)
	if err != nil {
		log.Println("Using default config")
	}

	spew.Dump(config)

	ctx := context.Background()

	client := binance.NewClient(config.APIKey, config.SecretKey)
	client.Debug = *debug

	client.NewExchangeInfoService().Symbols()

	listenKey, err := client.NewStartUserStreamService().Do(ctx)
	if err != nil {
		log.Fatalln(err)
	}
	go func() {
		for {
			time.Sleep(time.Minute * 30)
			log.Println("Pinging User Data Stream")
			err := client.NewKeepaliveUserStreamService().ListenKey(listenKey).Do(ctx)
			if err != nil {
				log.Fatalln(err)
			}
		}
	}()

	// trader
	tj := make(chan tradeJob)
	go func(tj *chan tradeJob) {
		for {
			select {
			case t := <-*tj:
				spew.Dump(t)

			}
		}
	}(&tj)

	wsHandler := func(e *binance.WsUserDataEvent) {
		spew.Dump(e)
		if e.Event == "balanceUpdate" {
			bu := e.BalanceUpdate
			change, err := strconv.ParseFloat(bu.Change, 64)
			if err != nil {
				log.Println("balance update change parse error", err)
				return
			}
			if change <= 0 {
				log.Println("skipping not positive balance update", bu.Asset, bu.Change)
				return
			}
			asset := strings.ToLower(bu.Asset)
			if _, ok := config.SellAll[asset]; !ok {
				log.Println("skipping not configured balance update", bu.Asset, bu.Change)
				return
			}
			to := strings.ToUpper(config.SellAll[asset])
			log.Println("trading balance update", bu.Change, bu.Asset, "to", to)

			tj <- tradeJob{
				From:   bu.Asset,
				To:     to,
				Amount: change,
			}
		}

	}

	errHandler := func(err error) {
		log.Println(err)
	}
	doneC, _, err := binance.WsUserDataServe(listenKey, wsHandler, errHandler)
	if err != nil {
		log.Println(err)
		return
	}
	<-doneC
}
