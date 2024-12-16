package client

import (
	"errors"
	"github.com/duke-git/lancet/v2/strutil"
	"github.com/jiang2012/caffeine/pkg/json"
	"github.com/jiang2012/caffeine/pkg/logging"
	"github.com/valyala/fasthttp"
	"go.uber.org/zap"
	"strconv"
	"time"
)

const (
	MethodGet  = fasthttp.MethodGet
	MethodPost = fasthttp.MethodPost
)

var httpClient = &fasthttp.Client{
	// 读超时时间，不设置read超时，可能会造成连接复用失效
	ReadTimeout: time.Second * 5,
	// 写超时时间
	WriteTimeout: time.Second * 5,
	// 5秒后，关闭空闲的活动连接
	MaxIdleConnDuration: time.Second * 5,
	// 当true时，从请求中去掉User-Agent标头
	NoDefaultUserAgentHeader: true,
	// 当true时，header中的key按照原样传输，默认会根据标准化转化
	DisableHeaderNamesNormalizing: true,
	//当true时，路径按原样传输，默认会根据标准化转化
	DisablePathNormalizing: true,
	Dial: (&fasthttp.TCPDialer{
		// 最大并发数，0表示无限制
		Concurrency: 4096,
		// 将 DNS 缓存时间从默认分钟增加到一小时
		DNSCacheDuration: time.Hour,
	}).Dial,
	MaxIdemponentCallAttempts: 3,
	RetryIfErr: func(request *fasthttp.Request, attempts int, err error) (resetTimeout bool, retry bool) {
		logging.Error("Request", zap.ByteString("url", request.URI().FullURI()), zap.Int("attempts", attempts), zap.Error(err))
		return true, true
	},
}

func HttpGet(url string, headerKeysAndValues ...string) ([]byte, error) {
	if len(headerKeysAndValues)%2 != 0 {
		return nil, errors.New("invalid header keys and values")
	}

	req, res := fasthttp.AcquireRequest(), fasthttp.AcquireResponse()
	defer func() {
		fasthttp.ReleaseRequest(req)
		fasthttp.ReleaseResponse(res)
	}()

	req.SetRequestURI(url)
	req.Header.SetMethod(fasthttp.MethodGet)
	for i := 0; i < len(headerKeysAndValues); i += 2 {
		req.Header.Set(headerKeysAndValues[i], headerKeysAndValues[i+1])
	}

	if err := httpClient.Do(req, res); err != nil {
		return nil, err
	} else {
		if res.StatusCode() != fasthttp.StatusOK {
			return nil, errors.New("unexpected status code: " + strconv.Itoa(res.StatusCode()))
		}

		return res.Body(), nil
	}
}

func HttpPostJson(url string, body []byte, headerKeysAndValues ...string) (map[string]interface{}, error) {
	if len(headerKeysAndValues)%2 != 0 {
		return nil, errors.New("invalid header keys and values")
	}

	req, res := fasthttp.AcquireRequest(), fasthttp.AcquireResponse()
	defer func() {
		fasthttp.ReleaseRequest(req)
		fasthttp.ReleaseResponse(res)
	}()

	req.SetRequestURI(url)
	req.Header.SetMethod(fasthttp.MethodPost)
	req.Header.SetContentTypeBytes([]byte("application/json"))
	for i := 0; i < len(headerKeysAndValues); i += 2 {
		req.Header.Set(headerKeysAndValues[i], headerKeysAndValues[i+1])
	}
	req.SetBodyRaw(body)

	if err := httpClient.Do(req, res); err != nil {
		return nil, err
	} else {
		if res.StatusCode() != fasthttp.StatusOK {
			return nil, errors.New("unexpected status code: " + strconv.Itoa(res.StatusCode()))
		}

		var result map[string]interface{}
		if err := json.UnmarshalFromString(strutil.BytesToString(res.Body()), &result); err != nil {
			return nil, err
		}
		return result, nil
	}
}
