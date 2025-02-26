package fingerprint

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	_ "embed"

	"github.com/axgle/mahonia"
	"github.com/panjf2000/ants/v2"
	"github.com/zan8in/afrog/pkg/config"
	"github.com/zan8in/afrog/pkg/core"
	"github.com/zan8in/afrog/pkg/poc"
	http2 "github.com/zan8in/afrog/pkg/protocols/http"
)

// reference https://github.com/0x727/FingerprintHub

type Service struct {
	Options     *config.Options
	fpSlice     []FingerPrint
	ResultSlice []Result
}

type Result struct {
	Url        string // 网址
	StatusCode string // 状态码
	Title      string // 标题
	Name       string // 指纹
}

type FingerPrint struct {
	Name           string            `json:"name"`
	Path           string            `json:"path"`
	RequestMethod  string            `json:"request_method"`
	RequestHeaders map[string]string `json:"request_headers"`
	RequestData    string            `json:"request_data"`
	StatusCode     int               `json:"status_code"`
	Headers        map[string]string `json:"headers"`
	Keyword        []string          `json:"keyword"`
	FaviconHash    []string          `json:"favicon_hash"`
	Priority       int               `json:"priority"`
}

var pTitle = regexp.MustCompile(`(?i:)<title>(.*?)</title>`)

//go:embed web_fingerprint_v3.json
var content []byte

func New(options *config.Options) (*Service, error) {
	var fpSlice []FingerPrint
	if err := json.Unmarshal(content, &fpSlice); err != nil {
		return nil, err
	}

	options.Count += len(options.Targets)

	return &Service{
		fpSlice: fpSlice,
		Options: options,
	}, nil
}

func (s *Service) Execute() {
	s.executeFingerPrintDetection()
}

func (s *Service) executeFingerPrintDetection() {
	if len(s.Options.Targets) > 0 {
		size := 100
		if s.Options.Config.FingerprintSizeWaitGroup > 0 {
			size = int(s.Options.Config.FingerprintSizeWaitGroup)
		}

		var wg sync.WaitGroup
		p, _ := ants.NewPoolWithFunc(size, func(wgTask any) {
			defer wg.Done()
			url := wgTask.(poc.WaitGroupTask).Value.(string)
			key := wgTask.(poc.WaitGroupTask).Key
			if s.Options.TargetLive.HandleTargetLive(url, -1) == -1 {
				// 该 url 在 targetlive 黑名单里面
				s.PrintColorResultInfoConsole(Result{})
			} else {
				// 该 url 未在 targetlive 黑名单里面
				s.processFingerPrintInputPair(url, key)
			}
		})
		defer p.Release()
		for k, target := range s.Options.Targets {
			wg.Add(1)
			_ = p.Invoke(poc.WaitGroupTask{Value: target, Key: k})
		}
		wg.Wait()
	}
}

func (s *Service) processFingerPrintInputPair(url string, key int) error {
	if len(s.fpSlice) == 0 {
		s.PrintColorResultInfoConsole(Result{})
		return nil
	}

	// 检测 http or https 并更新 targets 列表
	url, statusCode := http2.CheckTargetHttps2(url)
	if statusCode == -1 {
		// url 加入 targetlive 黑名单 +1
		s.Options.TargetLive.HandleTargetLive(url, 0)
		s.PrintColorResultInfoConsole(Result{})
		return nil
	} else {
		s.Options.TargetLive.HandleTargetLive(url, 1)
		s.Options.Targets[key] = url
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		s.PrintColorResultInfoConsole(Result{})
		return nil
	}

	data, headers, statuscode, err := http2.GetFingerprintRedirect(req)
	if err != nil {
		s.PrintColorResultInfoConsole(Result{})
		return nil
	}

	fpName := ""
	for _, v := range s.fpSlice {
		flag := false

		hflag := true
		if len(v.Headers) > 0 {
			hflag = false
			for k, h := range v.Headers {
				if len(headers[strings.ToLower(k)]) == 0 {
					hflag = false
					break
				}
				if len(headers[strings.ToLower(k)]) > 0 {
					if !strings.Contains(headers[strings.ToLower(k)][0], h) {
						hflag = false
						break
					}
					hflag = true
				}
			}
		}
		if len(v.Headers) > 0 && hflag {
			flag = true
		}

		kflag := true
		if len(v.Keyword) > 0 {
			kflag = false
			for _, k := range v.Keyword {
				if !strings.Contains(string(data), k) {
					kflag = false
					break
				}
				kflag = true
			}
		}
		if len(v.Keyword) > 0 && kflag {
			flag = true
		}

		if flag {
			fpName = v.Name
			break
		}
	}

	titleArr := pTitle.FindStringSubmatch(string(data))
	sTitle := ""
	if titleArr != nil {
		if len(titleArr) == 2 {
			sTitle = titleArr[1]
			if !utf8.ValidString(sTitle) {
				sTitle = mahonia.NewDecoder("gb18030").ConvertString(sTitle)
			}
		}
	}

	s.PrintColorResultInfoConsole(Result{Url: url, StatusCode: strconv.Itoa(statuscode), Title: sTitle, Name: fpName})

	return nil

}

func (s *Service) PrintColorResultInfoConsole(result Result) {
	r := &core.Result{}

	if len(result.StatusCode) != 0 {
		s.ResultSlice = append(s.ResultSlice, result)
		r.FingerResult = result
		r.IsVul = true
	}
	s.Options.ApiCallBack(r)
}
