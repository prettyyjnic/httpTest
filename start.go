package httpTest

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"strings"

	"regexp"

	simplejson "github.com/bitly/go-simplejson"
	iris "gopkg.in/kataras/iris.v4"
)

type servers struct {
	ServerArr []server `json:"servers"`
}

type server struct {
	Port    string `json:"port"`
	RuleArr []rule `json:"rule"`
}

type rule struct {
	Method    string   `json:"method"`
	Path      string   `json:"path"`
	ParamsArr []params `json:"params"`
	Response  interface{}
}

type params struct {
	Key   string      `json:"key"`
	Rule  string      `json:"rule"`
	Value interface{} `json:"value"`
}

type response struct {
	Code   int         `json:"code"`
	Error  string      `json:"error"`
	Result interface{} `json:"result"`
}

var configFile string

// Start start server
func Start(filename string) {
	configFile = filename
	_servers, err := unMarshalConfigFile(filename)
	if err != nil {
		fmt.Println(err.Error())
		return
	}
	_channels := make(chan int, len(_servers.ServerArr))
	for i := 0; i < len(_servers.ServerArr); i++ {
		go func(i int, _channels chan int) {
			defer func() { _channels <- 1 }()
			_servers.ServerArr[i].server()
		}(i, _channels)
	}
	for i := 0; i < len(_servers.ServerArr); i++ {
		<-_channels
	}
}

func unMarshalConfigFile(filename string) (servers, error) {
	var _servers servers
	var err error
	configDatas, err := ioutil.ReadFile(filename)
	if err != nil {
		return _servers, errors.New("read configs file error: " + err.Error())
	}
	err = json.Unmarshal(configDatas, &_servers)
	if err != nil {
		return _servers, errors.New("unmarshal config error: " + err.Error())
	}
	return _servers, nil
}

func checkRules(_method string, _params []params, ctx *iris.Context, json2 *simplejson.Json) (bool, error) {
	var err error
	_json := &simplejson.Json{}
	if json2 == nil {
		_json, err = simplejson.NewJson(ctx.PostBody())
	} else {
		_json = json2
	}
	if err != nil {
		return false, errors.New("read json error: " + err.Error())
	}
	for j := 0; j < len(_params); j++ {
		tmp := _params[j]

		switch tmp.Rule {
		case "eq":
			fallthrough
		case "EQ":
			// _value, err := tmp.Value.string
			if _method == "JSON" {
				if str, err := _json.Get(tmp.Key).String(); err != nil || str != tmp.Value.(string) {
					return false, nil
				}
			} else if ctx.Param(tmp.Key) != tmp.Value.(string) {
				return false, nil
			}
		case "regex":
			fallthrough
		case "REGEX":
			_reg, err := regexp.Compile(tmp.Value.(string))
			if err != nil {
				return false, errors.New("regexp compile error: " + tmp.Value.(string))
			}
			if strings.ToUpper(_method) == "JSON" {
				if !_reg.MatchString(_json.Get(tmp.Key).MustString()) {
					return false, nil
				}
			} else if !_reg.MatchString(ctx.Param(tmp.Key)) {
				return false, nil
			}
		case "params":
			fallthrough
		case "PARAMS":
			v, err := json.Marshal(tmp.Value)
			if err != nil {
				return false, err
			}
			var _tmpMapArr []params
			err = json.Unmarshal(v, &_tmpMapArr)
			if err != nil {
				return false, err
			}
			right, err := checkRules(_method, _tmpMapArr, ctx, _json.Get(tmp.Key))
			if err != nil {
				return false, err
			}
			if !right {
				return false, nil
			}
		default:
			return false, errors.New("unsupport rule:" + tmp.Rule)
		}
	}
	return true, nil
}

func (_server server) server() {
	app := iris.New()
	app.OnError(iris.StatusNotFound, func(ctx *iris.Context) {
		// get the config file every request time
		_serversConfigs, err := unMarshalConfigFile(configFile)
		_response := &response{}
		if err != nil {
			_response.Code = 500
			_response.Error = "unMarshalConfigFile error: " + err.Error()
			ctx.JSON(500, _response)
			return
		}
		var config server
		for i := 0; i < len(_serversConfigs.ServerArr); i++ {
			if _serversConfigs.ServerArr[i].Port == _server.Port {
				config = _serversConfigs.ServerArr[i]
			}
		}

		// match the path
		var matchIndexArr []int

		for i := 0; i < len(config.RuleArr); i++ {
			_rule := config.RuleArr[i]
			_path := ctx.RequestPath(false)
			_rule.Method = strings.ToUpper(_rule.Method)
			reg, err := regexp.Compile(_rule.Path)
			if err != nil {
				if _rule.Path != _path {
					continue
				}
				// check the method
				if _rule.Method == "GET" || _rule.Method == "POST" {
					if ctx.MethodString() != _rule.Method {
						continue
					}
				}
			} else if !reg.MatchString(_path) {
				continue
			}
			matchIndexArr = append(matchIndexArr, i)
		}
		// check the params

		if len(matchIndexArr) > 0 {
			for i := 0; i < len(matchIndexArr); i++ {
				_rule := config.RuleArr[matchIndexArr[i]]
				_rule.Method = strings.ToUpper(_rule.Method)
				flag, err := checkRules(_rule.Method, _rule.ParamsArr, ctx, nil)
				if err != nil {
					_response.Code = 500
					_response.Error = err.Error()
					ctx.JSON(500, _response)
				}
				if flag {
					ctx.JSON(200, _rule.Response)
					return
				}

			}
		}

		_response.Code = 404
		_response.Error = "No match path "
		ctx.JSON(404, _response)
	})
	app.Listen(":" + _server.Port)
}
