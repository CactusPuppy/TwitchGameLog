package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/cactuspuppy/twitchgamelog/secret"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

const ClientID = `ari2vux13uqzdxek5b4r1vw2vg80ix`
const TopicURL = `https://api.twitch.tv/helix/streams?user_id=`
const HookURL = `https://api.twitch.tv/helix/webhooks/hub`

type MainData struct {
	Streamer string
	CallbackURL string
	Port string
	ID string
	Token string
}

type StreamerData struct {
	Online bool
	Game string
	GameID string
	Title string
}

var DefaultConfig = map[string]interface{} {
	"streamer": "STREAMER NAME HERE",
	"callbackURL": "CALLBACK URL HERE",
	"port": "8080",
}
var Maindata MainData
var Streamerdata StreamerData
var Gamecache = make(map[string]string)
var cacheDisk = `cache.json`
var logFilename = ""
var client = http.Client{}
var seenNotifIDs = make(map[string]struct{}) //Make map to allow for easy contains check

//Fatally reports an error
func checkErrorFatal(err error) {
	if err != nil {
		log.Fatalln(err)
	}
}

//Non-fatally reports an error, and returns if there was no error
func checkError(err error) (ok bool) {
	if err != nil {
		log.Println(err)
		return false
	}
	return true
}

//Performs startup tasks
func main() {
	start := time.Now()
	log.Println("Starting TwitchGameLog v0.1 by CactusPuppy")

	//Get an access token
	token := getToken()
	Maindata.Token = token

	//Get config data
	if !getConfigData() {
		return
	}

	//Load game id cache
	loadGameCache()

	//Create logs directory if nonexistent
	logspath := filepath.Join(".", "logs")
	checkErrorFatal(os.MkdirAll(logspath, os.ModePerm))

	streamer := Maindata.Streamer
	callbackURL := Maindata.CallbackURL
	port := Maindata.Port

	//Get streamer ID
	id, err := getStreamerID(streamer, token, client)
	if err != nil {
		log.Fatalln("Error getting streamer:", err)
	}
	Maindata.ID = id
	log.Println("Now tracking",streamer,"(ID:",id+")")

	//Subscribe to proper webhook
	subToWebhook(callbackURL, id)

	//Perform initial query
	setupStreamerData()

	//Setup complete mark
	elapsed := time.Since(start)
	log.Printf("Startup complete, took %s\n", elapsed)

	//Start listening for webhook
	http.HandleFunc("/webhook", handleHook)
	err = http.ListenAndServe(":"+port, nil)
	checkErrorFatal(err)
}

func setupStreamerData() {
	//Form request for stream data
	request, err := http.NewRequest("GET", TopicURL+Maindata.ID, nil)
	checkError(err)
	//Add auth header
	request.Header.Set("Client-ID", ClientID)
	request.Header.Set("Authorization", "Bearer "+Maindata.Token)
	//Get response
	response, err := client.Do(request)
	checkError(err)
	checkRateLimit(response)
	body, err := ioutil.ReadAll(response.Body)
	checkError(err)
	//Extract response to JSON
	var responseJson map[string]interface{}
	err = json.Unmarshal(body, &responseJson)
	checkError(err)
	//Check if online
	responseData := responseJson["data"].([]interface{})
	if len(responseData) == 0 {
		Streamerdata.Online = false
		return
	}
	Streamerdata.Online = true
	//Get stream data
	streamData := responseData[0].(map[string]interface{})
	title := streamData["title"].(string)
	gameid := streamData["game_id"].(string)
	game, err := getGameFromId(gameid, client)
	checkError(err)
	msg := fmt.Sprintf("%s is now playing %s | Title: \"%s\"", Maindata.Streamer, game, title)
	logEvent(msg, time.Now())
	updateStreamer(title, game, gameid)
}

//Refresh hook
func refresh() {
	unsubFromWebhook(Maindata.CallbackURL, Maindata.ID)
	//Setup new log file
	logFilename = ""
	subToWebhook(Maindata.CallbackURL, Maindata.ID)
}

//Subscribes to the webhook
func subToWebhook(callbackURL string, id string) {
	payload := map[string]interface{}{
		"hub.callback":      callbackURL + "/webhook",
		"hub.mode":          "subscribe",
		"hub.topic":         TopicURL + id,
		"hub.lease_seconds": "864000",
		"hub.secret":        secret.PayloadSecret,
	}
	sendPayloadToHookHub(payload)
}

//Unsubscribes from webhook
func unsubFromWebhook(callbackURL string, id string) {
	payload := map[string]interface{}{
		"hub.callback":      callbackURL + "/webhook",
		"hub.mode":          "unsubscribe",
		"hub.topic":         TopicURL + id,
		"hub.secret":        secret.PayloadSecret,
	}
	sendPayloadToHookHub(payload)
}

func sendPayloadToHookHub(payload map[string]interface{}) {
	payloadBytes, err := json.Marshal(payload)
	checkErrorFatal(err)
	//Create request
	request, err := http.NewRequest("POST", HookURL, bytes.NewBuffer(payloadBytes))
	checkErrorFatal(err)
	//Add auth header
	request.Header.Set("Client-ID", ClientID)
	request.Header.Set("Authorization", "Bearer "+Maindata.Token)
	request.Header.Set("Content-Type", "application/json")
	//Perform request
	response, err := client.Do(request)
	checkErrorFatal(err)
	if response.StatusCode != 202 {
		log.Fatalln("Payload not accepted, HTTP code", string(response.StatusCode))
	}
}

// Put config data into main data if it can be found
// Returns whether the data was got and the program can continue
func getConfigData() (cont bool) {
	configPath := "config.json"
	//Check if config exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		log.Println(`No`, configPath, `file found, generating with defaults...
				PLEASE SET CONFIG VALUES BEFORE RESTARTING`)
		configBytes, err := json.MarshalIndent(DefaultConfig, "", "    ")
		checkErrorFatal(err)
		err = ioutil.WriteFile(configPath, configBytes, 0777)
		checkErrorFatal(err)
		return false
	}
	//Extract json from config
	data, err := ioutil.ReadFile(configPath)
	checkErrorFatal(err)
	var dataJson map[string]interface{}
	err = json.Unmarshal(data, &dataJson)
	checkErrorFatal(err)
	//Set maindata stuff
	Maindata.Streamer = dataJson["streamer"].(string)
	Maindata.CallbackURL = dataJson["callbackURL"].(string)
	Maindata.Port = dataJson["port"].(string)
	return true
}

//Retrieve streamer's Twitch ID
//If an error occurs, this method will echo it
func getStreamerID(streamer string, token string, client http.Client) (id string, err error) {
	//Form request for user data
	request, err := http.NewRequest("GET", "https://api.twitch.tv/helix/users?login="+streamer, nil)
	if err != nil {
		return "", err
	}
	//Add auth header
	request.Header.Set("Client-ID", ClientID)
	request.Header.Set("Authorization", "Bearer "+token)

	//Get response
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	checkRateLimit(response)
	body, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return "", err
	}

	//Extract response to JSON
	var responseJson map[string]interface{}
	err = json.Unmarshal(body, &responseJson)
	_ = response.Body.Close()
	if err != nil {
		return "", err
	}

	//Check user actually exists
	responseData := responseJson["data"]
	if len(responseData.([]interface{})) == 0 {
		log.Fatalln("Could not find streamer", streamer, ", aborting")
	}
	//Extract user ID
	firstData := responseData.([]interface{})[0].(map[string]interface{})
	id = firstData["id"].(string)
	return id, nil
}

//Gets an auth token
func getToken() string {
	response, err := http.Post("https://id.twitch.tv/oauth2/token"+
		"?client_id="+ClientID+
		"&client_secret="+secret.ClientSecret+
		"&grant_type=client_credentials", "application/json", nil)
	checkErrorFatal(err)
	if response.StatusCode != 200 {
		log.Fatal("Failed to get token, status code:",response.StatusCode)
		return ""
	}
	var resultJson map[string]interface{}
	err = json.NewDecoder(response.Body).Decode(&resultJson)
	checkErrorFatal(err)
	_ = response.Body.Close()
	token := resultJson["access_token"].(string)
	return token
}

func checkRateLimit(response *http.Response) {
	if response.StatusCode == 429 {
		//Find reset time
		i, err := strconv.ParseInt(response.Header.Get("Ratelimit-Reset"), 10, 64)
		checkErrorFatal(err)
		resetTime := time.Unix(i, 0)
		resetTimeString := resetTime.Format("3:04 PM")
		log.Printf("Twitch rate limit exceeded, cannot continue (Are you spamming?)\n"+
			"Rate limit will reset at: %s", resetTimeString)
	}
}

//Get a game by its ID and put it into the cache
func getGameFromId(id string, client http.Client) (name string, err error) {
	//Check cache first
	if val, ok := Gamecache[id]; ok {
		return val, nil
	}

	//Start process to get game ID
	token := Maindata.Token
	if token == "" {
		log.Fatalln("No auth token found")
	}
	//Form request for user data
	request, err := http.NewRequest("GET", "https://api.twitch.tv/helix/games?id="+id, nil)
	checkErrorFatal(err)
	//Add auth header
	request.Header.Set("Client-ID", ClientID)
	request.Header.Set("Authorization", "Bearer "+token)

	//Get response
	response, err := client.Do(request)
	checkErrorFatal(err)
	if response.StatusCode != 200 {
		msg := fmt.Sprint("Error querying game from Twitch API, HTTP Code ", response.StatusCode)
		log.Println(msg)
		return "", errors.New(msg)
	}
	checkRateLimit(response)
	body, err := ioutil.ReadAll(response.Body)
	checkErrorFatal(err)

	//Extract response to JSON
	var responseJson map[string]interface{}
	err = json.Unmarshal(body, &responseJson)
	_ = response.Body.Close()
	checkErrorFatal(err)

	//Check user actually exists
	responseData := responseJson["data"]
	if len(responseData.([]interface{})) == 0 {
		msg := fmt.Sprint("Could not find game with ID", id)
		log.Println(msg)
		return "", errors.New(msg)
	}
	//Extract game name
	firstData := responseData.([]interface{})[0].(map[string]interface{})
	name = firstData["name"].(string)
	Gamecache[id] = name
	saveGameCache()
	return name, nil
}

//Saves the Gamecache to disk
func saveGameCache() {
	cacheBytes, err := json.MarshalIndent(&Gamecache, "", "    ")
	checkErrorFatal(err)
	//Check if there is a game cache
	if _, err := os.Stat(cacheDisk); os.IsNotExist(err) {
		log.Println(`No game cache file found, creating...`)
		err = ioutil.WriteFile(cacheDisk, cacheBytes, 0777)
		checkErrorFatal(err)
		return
	}
	//Else append the new addition to the file
	file, err := os.OpenFile(cacheDisk, os.O_RDWR, 0644)
	checkErrorFatal(err)
	defer file.Close()
	err = file.Truncate(0)
	checkErrorFatal(err)
	_, err = file.Seek(0, 0)
	checkErrorFatal(err)
	_, err = file.WriteAt(cacheBytes, 0)
	checkErrorFatal(err)
	err = file.Sync()
	checkErrorFatal(err)
}

//Loads the Gamecache from disk
//If no Gamecache, fails silently
func loadGameCache() {
	//Check there is a cache file
	if _, err := os.Stat(cacheDisk); os.IsNotExist(err) {
		return
	}
	//Extract json from file
	data, err := ioutil.ReadFile(cacheDisk)
	checkErrorFatal(err)
	err = json.Unmarshal(data, &Gamecache)
	checkErrorFatal(err)
}

//Handles when the webhook issues a payload
func handleHook(w http.ResponseWriter, r *http.Request) {
	//Acknowledge reception
	w.WriteHeader(http.StatusOK)
	//Respond to challenge query
	if r.Method == "GET" || r.Method == "" { //GET only on challenge
		//Get query
		query := r.URL.Query()
		if query["hub.mode"][0] == "denied" {
			_, err := fmt.Fprintf(w, "200 OK", nil)
			checkErrorFatal(err)
			log.Println("Subscription to webhook was denied")
			return
		}
		if query["hub.mode"][0] == "subscribe" && !checkRequest(query) {
			log.Println("Did not get same subscription back")
			return
		}
		challenge := query["hub.challenge"][0]
		_, err := w.Write([]byte(challenge))
		checkErrorFatal(err)
		return
	}
	signature := r.Header.Get("X-Hub-Signature")
	//Check for duplicate notification (include signature because weirdness)
	notifID := r.Header.Get("Twitch-Notification-Id") + signature
	if _, ok := seenNotifIDs[notifID]; ok {
		//Seen before, ignore
		return
	}
	seenNotifIDs[notifID+signature] = struct{}{}
	//Check signature of payload
	bodyBytes, err := ioutil.ReadAll(r.Body)
	h := hmac.New(sha256.New, []byte(secret.PayloadSecret))
	h.Write(bodyBytes)
	hash := "sha256="+hex.EncodeToString(h.Sum(nil))
	if hash != signature {
		log.Fatalln("Signature invalid, header:", signature, "| calculated hash:", hash)
	}
	//Dealing with a notificaton, get timestamp
	timestamp := time.Now()

	//Extract JSON payload
	payload := make(map[string]interface{})
	err = json.Unmarshal(bodyBytes, &payload)
	checkErrorFatal(err)

	data := payload["data"].([]interface{})
	//Check if went offline
	if len(data) == 0 {
		Streamerdata.Online = false
		logMsg := fmt.Sprintf("%s went offline", Maindata.Streamer)
		logEvent(logMsg, timestamp)
		refresh()
		return
	}
	//Get additional data
	streamData := data[0].(map[string]interface{})
	title := streamData["title"].(string)
	gameid := streamData["game_id"].(string)
	game, err := getGameFromId(gameid, client)
	if err != nil {
		checkErrorResponse(err, w)
	}
	//Check if start of stream
	if !Streamerdata.Online {
		Streamerdata.Online = true
		logMsg := fmt.Sprintf("%s started streaming %s | Title: \"%s\"", Maindata.Streamer, game, title)
		logEvent(logMsg, timestamp)
		updateStreamer(title, game, gameid)
		return
	}
	//Mark change
	if gameid != Streamerdata.GameID { //Changed games
		logMsg := fmt.Sprintf("%s switched to %s | Title: \"%s\"", Maindata.Streamer, game, title)
		logEvent(logMsg, timestamp)
		updateStreamer(title, game, gameid)
	} else if title != Streamerdata.Title {
		logMsg := fmt.Sprintf("%s changed stream title | Title: \"%s\"", Maindata.Streamer, title)
		logEvent(logMsg, timestamp)
		updateStreamer(title, game, gameid)
	}
}

//Updates Streamerdata with appropriate data
func updateStreamer(title string, game string, gameid string) {
	Streamerdata.Title = title
	Streamerdata.Game = game
	Streamerdata.GameID = gameid
}

//Checks for an error, and if err != nil, write
func checkErrorResponse(err error, w http.ResponseWriter) {
	if err != nil {
		checkError(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

//Checks that the request is what we requested
func checkRequest(values url.Values) bool {
	return values["hub.topic"][0] == TopicURL + Maindata.ID
}

func logEvent(message string, timestamp time.Time) {
	log.Println(message)
	//Check the logfile name has been set
	if logFilename == "" {
		logFilename = "logs/"+timestamp.Format("2006-01-02_15-04-05")+".log"
	}
	//Open the log file for appending
	f, err := os.OpenFile(logFilename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	checkErrorFatal(err)
	defer f.Close()
	//Prepend time to message
	message = timestamp.Format("[Jan 2, 2006 | 15:04:05] ") + message + "\n"
	//Log message
	_, err = f.Write([]byte(message))
	checkErrorFatal(err)
}