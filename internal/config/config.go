package config

import (
	"github.com/duke-git/lancet/v2/strutil"
	"github.com/jiang2012/caffeine/pkg/logging"
	"github.com/spf13/viper"
	"strconv"
	"strings"
)

type Config struct {
	Log *logging.Config
	*HttpServer
	StreamNumLimitPerWsConn int
	Exchanges               []map[string]string
	SymbolNumLimit          int
}

type HttpServer struct {
	PprofPort int
}

type Binance struct {
	Name               string
	RestUrl            string
	SingleStreamUrl    string
	CombinedStreamsUrl string
	SymbolsUrlPath     string
	Enabled            bool
}

type Okx struct {
	Name                                string
	RestUrl                             string
	ApiKey                              string
	SecretKey                           string
	PassPhrase                          string
	PublicStreamUrl                     string
	PrivateStreamUrl                    string
	BusinessStreamUrl                   string
	PublicStreamChannels                []string
	PrivateStreamChannels               []string
	BusinessStreamChannels              []string
	GetPositionsUrlPath                 string
	ClosePositionUrlPath                string
	SymbolsUrlPath                      string
	SetLeverageUrlPath                  string
	PlaceOrderUrlPath                   string
	GetOrderInfoUrlPath                 string
	PlaceTakeProfitStopLossOrderUrlPath string
	Enabled                             bool
}

func (c *Config) GetBinance() *Binance {
	d := c.Exchanges[0]

	enabled, _ := strconv.ParseBool(d["enabled"])
	return &Binance{
		Name:               d["name"],
		RestUrl:            d[strings.ToLower("restUrl")],
		SingleStreamUrl:    d[strings.ToLower("singleStreamUrl")],
		CombinedStreamsUrl: d[strings.ToLower("combinedStreamsUrl")],
		SymbolsUrlPath:     d[strings.ToLower("symbolsUrlPath")],
		Enabled:            enabled,
	}
}

func (c *Config) GetOkx() *Okx {
	d := c.Exchanges[1]

	enabled, _ := strconv.ParseBool(d["enabled"])
	return &Okx{
		Name:                                d["name"],
		RestUrl:                             d[strings.ToLower("restUrl")],
		ApiKey:                              d[strings.ToLower("apiKey")],
		SecretKey:                           d[strings.ToLower("secretKey")],
		PassPhrase:                          d[strings.ToLower("passphrase")],
		PublicStreamUrl:                     d[strings.ToLower("publicStreamUrl")],
		PrivateStreamUrl:                    d[strings.ToLower("privateStreamUrl")],
		BusinessStreamUrl:                   d[strings.ToLower("businessStreamUrl")],
		PublicStreamChannels:                strutil.SplitAndTrim(d[strings.ToLower("publicStreamChannels")], ","),
		PrivateStreamChannels:               strutil.SplitAndTrim(d[strings.ToLower("privateStreamChannels")], ","),
		BusinessStreamChannels:              strutil.SplitAndTrim(d[strings.ToLower("businessStreamChannels")], ","),
		SymbolsUrlPath:                      d[strings.ToLower("symbolsUrlPath")],
		GetPositionsUrlPath:                 d[strings.ToLower("getPositionsUrlPath")],
		ClosePositionUrlPath:                d[strings.ToLower("closePositionUrlPath")],
		SetLeverageUrlPath:                  d[strings.ToLower("setLeverageUrlPath")],
		PlaceOrderUrlPath:                   d[strings.ToLower("placeOrderUrlPath")],
		GetOrderInfoUrlPath:                 d[strings.ToLower("getOrderInfoUrlPath")],
		PlaceTakeProfitStopLossOrderUrlPath: d[strings.ToLower("placeTakeProfitStopLossOrderUrlPath")],
		Enabled:                             enabled,
	}
}

func Init() *Config {
	viper.AddConfigPath("./")
	if err := viper.ReadInConfig(); err != nil {
		panic(err)
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		panic(err)
	}

	return &cfg
}
