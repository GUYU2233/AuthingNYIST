package libauth

import (
	"crypto/hmac"
	"crypto/md5"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/juju/loggo"
)

var logger = loggo.GetLogger("libauth")

func extractJSONFromJSONP(jsonp, callbackName string) (string, error) {
	l := len(callbackName)
	if len(jsonp) < l+2 {
		return "", errors.New("JSONP string too short")
	}
	if jsonp[:l] != callbackName || jsonp[l] != '(' || jsonp[len(jsonp)-1] != ')' {
		return "", errors.New("Invalid format")
	}
	return jsonp[l+1 : len(jsonp)-1], nil
}

func buildChallengeParams(username string, anotherIP string) url.Values {

	challParams := url.Values{
		"username":     []string{username},
		"ip":           []string{anotherIP},
		"double_stack": []string{"1"},
	}

	return challParams
}

func buildLoginParams(username, password, token string, logout bool, anotherIP string, acID string) (loginParams url.Values, err error) {
	ip := anotherIP
	mac := hmac.New(md5.New, []byte(token))
	_, _ = mac.Write([]byte(password))
	hmd5 := fmt.Sprintf("%032x", mac.Sum(nil))

	action := "login"
	rawInfo := map[string]string{
		"username": username,
		"password": password,
		"ip":       ip,
		"acid":     acID,
		"enc_ver":  "s" + "run" + "_bx1",
	}
	if logout {
		action = "logout"
		delete(rawInfo, "password")
	}
	infoJSON, _ := json.Marshal(rawInfo)

	loginParams = url.Values{
		"action":       []string{action},
		"ac_id":        []string{acID},
		"n":            []string{"200"},
		"type":         []string{"1"},
		"ip":           []string{ip},
		"double_stack": []string{"1"},
		"username":     []string{username},
	}
	if !logout {
		loginParams.Add("password", "{MD5}"+hmd5)
	}
	encoded := XEncode(string(infoJSON), token)
	if encoded == nil {
		err = errors.New("XEncode failed")
		return
	}
	loginParams.Add("info", "{SRBX1}"+QuirkBase64Encode(*encoded))

	if logout {
		loginParams.Add("chksum", sha1sum(token+username+token+acID+token+ip+token+loginParams.Get("n")+token+loginParams.Get("type")+token+loginParams.Get("info")))
	} else {
		loginParams.Add("chksum", sha1sum(token+username+token+hmd5+token+acID+token+ip+token+loginParams.Get("n")+token+loginParams.Get("type")+token+loginParams.Get("info")))
	}

	return
}

func GetJSON(baseUrl string, params url.Values) (string, error) {
	const CB = "C_a_l_l_b_a_c_k"
	params.Set("callback", CB)
	var netClient = &http.Client{
		Timeout: time.Second * 2,
	}
	url := baseUrl + "?" + params.Encode()
	logger.Debugf("GET \"%s\"\n", url)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "auth-nyist-cli")
	logger.Debugf("User-Agent: auth-nyist-cli\n")

	resp, err := netClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return extractJSONFromJSONP(string(body), CB)
}

func IsOnline(host *UrlProvider, acID string) (online bool, err error, username string) {
	// Current portals expose status directly instead of redirecting to succeed_wired.php.
	if body, statusErr := GetJSON(host.protocol+host.host+"/cgi-bin/rad_user_info", url.Values{}); statusErr == nil {
		var status struct {
			Error    string `json:"error"`
			Username string `json:"user_name"`
		}
		if json.Unmarshal([]byte(body), &status) == nil && status.Error == "ok" && status.Username != "" {
			return true, nil, status.Username
		}
	}
	var netClient = &http.Client{
		Timeout: time.Second * 2,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			logger.Debugf("REDIRECT \"%v\"\n", req.URL)
			if strings.Index(req.URL.Path, "succeed_wired.php") != -1 {
				online = true
				regexUsername := regexp.MustCompile(`username=([-a-zA-Z0-9]+)`)
				matches := regexUsername.FindStringSubmatch(req.URL.RawQuery)
				if len(matches) > 0 {
					username = matches[1]
					logger.Debugf("Extracted username: \"%s\"\n", username)
				}

			}
			return nil
		},
	}
	online = false
	params := url.Values{
		"ac_id": []string{acID},
	}
	url := host.OnlineCheckUriBase() + "?" + params.Encode()
	logger.Debugf("GET \"%s\"\n", url)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return
	}
	req.Header.Set("User-Agent", "auth-nyist-cli")
	logger.Debugf("User-Agent: auth-nyist-cli\n")

	resp, err := netClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	return
}

func LoginLogout(username, password string, host *UrlProvider, logout bool, anotherIP string, acID string) (err error) {
	logger.Debugf("Getting challenge...\n")
	body, err := GetJSON(host.ChallengeUriBase(), buildChallengeParams(username, anotherIP))
	if err != nil {
		return
	}
	logger.Debugf("Challenge response: %v\n", body)

	var challResp map[string]interface{}
	err = json.Unmarshal([]byte(body), &challResp)
	if err != nil {
		return
	}
	res, valid := challResp["res"].(string)
	if !valid || res != "ok" {
		err = errors.New("Failed to get challenge: " + res)
		return
	}
	token, valid := challResp["challenge"].(string)
	if !valid {
		err = errors.New("No challenge field")
		return
	}

	if anotherIP == "" {
		anotherIP, _ = challResp["client_ip"].(string)
	}
	loginParams, err := buildLoginParams(username, password, token, logout, anotherIP, acID)
	if err != nil {
		return
	}
	logger.Debugf("Sending login request...\n")
	body, err = GetJSON(host.LoginUriBase(), loginParams)
	if err != nil {
		return
	}
	logger.Debugf("Login response: %v\n", body)
	var loginResp map[string]interface{}
	err = json.Unmarshal([]byte(body), &loginResp)
	if err != nil {
		return
	}
	res, valid = loginResp["error"].(string)
	if !valid {
		err = errors.New("No error field")
		return
	}

	if res == "ok" {
		err = nil
	} else {
		ecode, _ := loginResp["ecode"].(string)
		if strerr, exist := PortalError[ecode]; exist {
			err = errors.New(strerr)
		} else {
			err = errors.New(res)
		}
	}

	return
}
