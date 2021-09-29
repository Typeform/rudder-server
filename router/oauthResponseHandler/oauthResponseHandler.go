package oauthResponseHandler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rudderlabs/rudder-server/config"
	backendconfig "github.com/rudderlabs/rudder-server/config/backend-config"
	router_utils "github.com/rudderlabs/rudder-server/router/utils"
	"github.com/rudderlabs/rudder-server/services/stats"
	"github.com/rudderlabs/rudder-server/utils/logger"
)

type AccountSecret struct {
	AccessToken    string `json:"accessToken"`
	ExpirationDate string `json:"expirationDate"`
}

// OAuthErrResHandler is the handle for this class
type OAuthErrResHandler struct {
	tr                             *http.Transport
	client                         *http.Client
	oauthErrHandlerReqTimerStat    stats.RudderStats
	oauthErrHandlerNetReqTimerStat stats.RudderStats
	logger                         logger.LoggerI
	destLockMap                    map[string]*sync.RWMutex // This mutex map is used for disable destination locking
	accountLockMap                 map[string]*sync.RWMutex // This mutex map is used for refresh token locking
	lockMapWMutex                  sync.RWMutex             // This mutex is used to prevent concurrent writes in lockMap(s) mentioned in the struct
}

type Authorizer interface {
	Setup()
	DisableDestination(destination backendconfig.DestinationT, workspaceId string) (statusCode int, resBody string)
	RefreshToken(workspaceId string, accountId string, accessToken string) (statusCode int, resBody string)
}

type ControlPlaneRequestT struct {
	Body        []byte
	ContentType string
	Url         string
	Method      string
}

// This function creates a new OauthErrorResponseHandler
func NewOAuthErrorHandler() *OAuthErrResHandler {
	return &OAuthErrResHandler{}
}

var (
	configBEURL      string
	pkgLogger        logger.LoggerI
	loggerNm         string
	workspaceToken   string
	isMultiWorkspace bool
)

const (
	DISABLE_DEST  = "DISABLE_DESTINATION"
	REFRESH_TOKEN = "REFRESH_TOKEN"
)

// The response from the transformer network layer will be sent with a output property(in case of an error)
type ErrorOutput struct {
	Output ErrorResponse `json:"output"`
}

// This struct represents the datastructure present in Transformer network layer Error builder
type ErrorResponse struct {
	Message           string                 `json:"message"`
	Destination       map[string]interface{} `json:"destination"`
	NetworkFailure    bool                   `json:"networkFailure"`
	Status            int                    `json:"status"`
	AuthErrorCategory string                 `json:"authErrorCategory"`
	AccessToken       string                 `json:"accessToken"`
}

type RefreshTokenBody struct {
	hasExpired   bool
	expiredToken string
}

// Custom Marshalling for RefreshTokenBody struct
func (refTokenBody RefreshTokenBody) MarshalJSON() ([]byte, error) {
	j, err := json.Marshal(struct {
		HasExpired   bool
		ExpiredToken string
	}{
		HasExpired:   refTokenBody.hasExpired,
		ExpiredToken: refTokenBody.expiredToken,
	})
	if err != nil {
		return nil, err
	}
	return j, nil
}

func Init() {
	configBEURL = backendconfig.GetConfigBackendURL()
	pkgLogger = logger.NewLogger().Child("router").Child("OAuthResponseHandler")
	loggerNm = "OAuthResponseHandler"
	workspaceToken = config.GetWorkspaceToken()
	isMultiWorkspace = config.GetEnvAsBool("HOSTED_SERVICE", false)
	if isMultiWorkspace {
		workspaceToken = config.GetEnv("HOSTED_SERVICE_SECRET", "password")
	}
}

func (authErrHandler *OAuthErrResHandler) Setup() {
	authErrHandler.logger = pkgLogger
	authErrHandler.tr = &http.Transport{}
	//This timeout is kind of modifiable & it seemed like 10 mins for this is too much!
	authErrHandler.client = &http.Client{Transport: authErrHandler.tr, Timeout: 5 * time.Minute}
	authErrHandler.oauthErrHandlerReqTimerStat = stats.NewStat("router.processor.oauthErrorHandler_request_time", stats.TimerType)
	authErrHandler.oauthErrHandlerNetReqTimerStat = stats.NewStat("router.oauthErrorHandler_network_request_time", stats.TimerType)
	authErrHandler.destLockMap = make(map[string]*sync.RWMutex)
	authErrHandler.accountLockMap = make(map[string]*sync.RWMutex)
	authErrHandler.lockMapWMutex = sync.RWMutex{}
}

func (authErrHandler *OAuthErrResHandler) RefreshToken(workspaceId string, accountId string, accessToken string) (statusCode int, respBody string) {
	authErrHandler.oauthErrHandlerReqTimerStat.Start()
	if !router_utils.IsNotEmptyString(accessToken) {
		return http.StatusBadRequest, `Cannot proceed with refresh token request as accessToken is empty`
	}
	resMgrErr := authErrHandler.NewMutex(accountId, REFRESH_TOKEN)
	if resMgrErr != nil {
		panic(resMgrErr)
	}
	authErrHandler.oauthErrHandlerNetReqTimerStat.Start()
	refreshUrl := fmt.Sprintf("%s/dest/workspaces/%s/accounts/%s/token", configBEURL, workspaceId, accountId)
	authErrHandler.oauthErrHandlerNetReqTimerStat.End()
	refTokenBody := RefreshTokenBody{
		hasExpired:   true,
		expiredToken: accessToken,
	}
	res, err := json.Marshal(refTokenBody)
	if err != nil {
		panic(err)
	}
	refreshCpReq := &ControlPlaneRequestT{
		Method:      http.MethodPost,
		Url:         refreshUrl,
		ContentType: "application/json; charset=utf-8",
		Body:        []byte(res),
	}

	authErrHandler.accountLockMap[accountId].Lock()
	statusCode, response := authErrHandler.cpApiCall(refreshCpReq)
	authErrHandler.accountLockMap[accountId].Unlock()

	authErrHandler.oauthErrHandlerReqTimerStat.End()
	authErrHandler.logger.Debugf("[%s request] :: Refresh token response received : %s", loggerNm, response)
	return statusCode, response
}

func (authErrHandler *OAuthErrResHandler) DisableDestination(destination backendconfig.DestinationT, workspaceId string) (statusCode int, respBody string) {
	authErrHandler.oauthErrHandlerReqTimerStat.Start()
	destinationId := destination.ID
	resMgrErr := authErrHandler.NewMutex(destinationId, DISABLE_DEST)
	if resMgrErr != nil {
		panic(resMgrErr)
	}
	disableURL := fmt.Sprintf("%s/workspaces/%s/destinations/%s/disable", configBEURL, workspaceId, destinationId)
	disableCpReq := &ControlPlaneRequestT{
		Url:    disableURL,
		Method: http.MethodDelete,
	}

	authErrHandler.destLockMap[destinationId].Lock()
	statusCode, respBody = authErrHandler.cpApiCall(disableCpReq)
	authErrHandler.destLockMap[destinationId].Unlock()

	authErrHandler.oauthErrHandlerReqTimerStat.End()
	authErrHandler.logger.Infof("[%s request] :: Disable Response received : %s\n", loggerNm)
	return statusCode, respBody
}

func processResponse(resp *http.Response) (statusCode int, respBody string) {
	var respData []byte
	var ioUtilReadErr error
	if resp != nil && resp.Body != nil {
		respData, ioUtilReadErr = ioutil.ReadAll(resp.Body)
		defer resp.Body.Close()
		if ioUtilReadErr != nil {
			return http.StatusInternalServerError, ioUtilReadErr.Error()
		}
	}
	//Detecting content type of the respData
	contentTypeHeader := http.DetectContentType(respData)
	//If content type is not of type "*text*", overriding it with empty string
	if !(strings.Contains(strings.ToLower(contentTypeHeader), "text") ||
		strings.Contains(strings.ToLower(contentTypeHeader), "application/json") ||
		strings.Contains(strings.ToLower(contentTypeHeader), "application/xml")) {
		respData = []byte("")
	}

	return resp.StatusCode, string(respData)
}

func (authErrHandler *OAuthErrResHandler) cpApiCall(cpReq *ControlPlaneRequestT) (int, string) {
	var reqBody *bytes.Buffer
	var req *http.Request
	var err error
	if cpReq.Body != nil {
		reqBody = bytes.NewBuffer(cpReq.Body)
		req, err = http.NewRequest(cpReq.Method, cpReq.Url, reqBody)
	} else {
		req, err = http.NewRequest(cpReq.Method, cpReq.Url, nil)
	}
	if err != nil {
		authErrHandler.logger.Errorf("[%s request] :: destination request failed: %+v\n", loggerNm, err)
		// Abort on receiving an error in request formation
		return http.StatusBadRequest, err.Error()
	}
	// Authorisation setting
	req.SetBasicAuth(workspaceToken, "")
	authErrHandler.oauthErrHandlerNetReqTimerStat.Start()
	res, doErr := authErrHandler.client.Do(req)
	authErrHandler.oauthErrHandlerNetReqTimerStat.End()
	authErrHandler.logger.Debugf("[%s request] :: destination request sent\n", loggerNm)
	if doErr != nil {
		// Abort on receiving an error
		authErrHandler.logger.Errorf("[%s request] :: destination request failed: %+v\n", loggerNm, doErr)
		return http.StatusBadRequest, doErr.Error()
	}
	if res.Body != nil {
		defer res.Body.Close()
	}
	statusCode, resp := processResponse(res)
	return statusCode, resp
}

func (resHandler *OAuthErrResHandler) NewMutex(id string, errCategory string) error {
	var mutexMap map[string]*sync.RWMutex
	switch errCategory {
	case DISABLE_DEST:
		mutexMap = resHandler.destLockMap
	case REFRESH_TOKEN:
		mutexMap = resHandler.accountLockMap
	}
	(&resHandler.lockMapWMutex).Lock()
	defer (&resHandler.lockMapWMutex).Unlock()
	if mutexMap != nil {
		if _, ok := mutexMap[id]; !ok {
			resHandler.logger.Infof("[%s request] :: Creating new mutex for %s\n", loggerNm, id)
			mutexMap[id] = &sync.RWMutex{}
		}
		resHandler.logger.Infof("[%s request] :: Already created mutex for %s\n", loggerNm, id)
		return nil
	}
	resHandler.logger.Infof("[%s request] :: Case missing for mutex for %s\n", loggerNm, id)
	return fmt.Errorf(`except %v, %v error category is not supported`, DISABLE_DEST, REFRESH_TOKEN)
}