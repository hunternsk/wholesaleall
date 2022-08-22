package main

import (
	"context"
	"encoding/json"
	"flag"
	"github.com/adshao/go-binance/v2"
	"github.com/davecgh/go-spew/spew"
	"log"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type configType struct {
	APIKey    string                        `json:"api_key"`
	SecretKey string                        `json:"secret_key"`
	SellAll   map[string]map[string]float64 `json:"sell_all"`
}

type tradeJob struct {
	From     string
	To       string
	Amount   float64
	CrossJob *tradeJob
}

var (
	configFile = flag.String("c", "config.json", "config filename")
	debug      = flag.Bool("debug", false, "debug")

	config = configType{
		APIKey:    "",
		SecretKey: "",
		SellAll:   map[string]map[string]float64{"eth": {"usdt": 100}},
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

	exchangeInfo, err := client.NewExchangeInfoService().Do(ctx)
	if err != nil {
		log.Fatalln("error getting exchange info", err)
	}

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
				log.Println(err)
				time.Sleep(time.Minute)
			}
		}
	}()

	// trader
	tj := make(chan *tradeJob)
	go func(tj chan *tradeJob) {
		for {
			select {
			case t := <-tj:
				//spew.Dump(t)
				var orderService *binance.CreateOrderService
				getPrecision := func(s binance.Symbol) int {
					p := 0
					for _, f := range s.Filters {
						if f["filterType"].(string) == "LOT_SIZE" {
							stepSize := f["stepSize"].(string)
							step, _ := strconv.ParseFloat(stepSize, 8)
							if step > 0 {
								p = int(math.Abs(math.Round(math.Log10(step))))
							}
							break
						}
					}
					return p
				}

				for _, s := range exchangeInfo.Symbols {
					if s.Symbol == t.From+t.To {
						p := getPrecision(s)
						orderService = client.NewCreateOrderService().
							Symbol(s.Symbol).
							Side(binance.SideTypeSell).
							Type("MARKET").
							Quantity(strconv.FormatFloat(t.Amount, 'f', p, 64))
						break
					}
					if s.Symbol == t.To+t.From {
						p := getPrecision(s)
						orderService = client.NewCreateOrderService().
							Symbol(s.Symbol).
							Side(binance.SideTypeBuy).
							Type("MARKET").
							QuoteOrderQty(strconv.FormatFloat(t.Amount, 'f', p, 64))
						break
					}
				}
				if orderService != nil {
					//todo: check trade rules
					//log.Println("trading")
					res, err := orderService.Do(ctx)
					if err != nil {
						log.Println("create order error", err)
						continue
					}
					//spew.Dump(res)
					quoteQuantity, err := strconv.ParseFloat(res.CummulativeQuoteQuantity, 64)
					if err != nil {
						log.Println("res.CummulativeQuoteQuantity parse error", err)
						return
					}
					log.Println("executed", res.Symbol, res.Side, quoteQuantity)
					if t.CrossJob != nil {
						log.Println("crossjob:", t.CrossJob, "trading")
						go func() {
							tj <- &tradeJob{
								From:     "USDT",
								To:       t.CrossJob.To,
								Amount:   quoteQuantity,
								CrossJob: nil,
							}
						}()
					}
					continue
				}

				log.Println("direct symbol not found, trading via usdt")
				go func() {
					tj <- &tradeJob{
						From:     t.From,
						To:       "USDT",
						Amount:   t.Amount,
						CrossJob: t,
					}
				}()
			}
		}
	}(tj)

	wsHandler := func(e *binance.WsUserDataEvent) {
		//spew.Dump(e)
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
			log.Println("balance updated:", bu.Change, bu.Asset)
			for to, percent := range config.SellAll[asset] {
				to = strings.ToUpper(to)
				log.Printf("trading %f%% of %s to %s", percent, bu.Asset, to)
				tj <- &tradeJob{
					From:   bu.Asset,
					To:     to,
					Amount: change / 100 * percent,
				}
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
