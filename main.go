package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/cactuspuppy/twitchgamelog/secret"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"
)

const ClientID = `ari2vux13uqzdxek5b4r1vw2vg80ix`
const TopicURL = `https://api.twitch.tv/helix/streams?user_id=`

var DefaultConfig = map[string]interface{} {
	"streamer": "STREAMER NAME HERE",
	"callbackURL": "CALLBACK URL HERE",
	"port": "8080",
}

type MainData struct {
	Streamer string
	CallbackURL string
	Port string
	ID string
	Token string
	Online bool
}

var maindata MainData

func checkError(err error) {
	if err != nil {
		log.Fatalln(err)
	}
}

//Performs startup tasks
func main() {
	start := time.Now()
	log.Println("Starting TwitchGameLog v0.1 by CactusPuppy")

	//Get our http client
	client := http.Client{}
	//Get an access token
	token := getToken()
	maindata.Token = token

	//TODO: Get config values
	maindata.Streamer = "cactuspupbot"
	maindata.CallbackURL = "https://480ba225.ngrok.io"
	maindata.Port = "8080"
	maindata.Online = false

	if !getConfigData() {
		return
	}

	streamer := maindata.Streamer
	callbackURL := maindata.CallbackURL
	port := maindata.Port

	//Get streamer ID
	id := getStreamerID(streamer, token, client)
	maindata.ID = id
	log.Println("Now tracking",streamer,"(ID:",id+")")

	//Subscribe to proper webhook
	hookURL := `https://api.twitch.tv/helix/webhooks/hub` //TODO
	payload := map[string]interface{}{
		"hub.callback":      callbackURL+"/webhook",
		"hub.mode":          "subscribe",
		"hub.topic":         TopicURL + id,
		"hub.lease_seconds": "1000",
		"hub.secret":        secret.PayloadSecret,
	}
	payloadBytes, err := json.Marshal(payload)
	checkError(err)

	//Create request
	request, err := http.NewRequest("POST", hookURL, bytes.NewBuffer(payloadBytes))
	checkError(err)
	//Add auth header
	request.Header.Set("Client-ID", ClientID)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	//Perform request
	response, err := client.Do(request)
	checkError(err)
	if response.StatusCode != 202 {
		log.Fatalln("Payload not accepted, HTTP code", string(response.StatusCode))
	}

	elapsed := time.Since(start)
	log.Printf("Startup complete, took %s\n", elapsed)

	//Start listening for webhook
	http.HandleFunc("/webhook", handleHook)
	err = http.ListenAndServe(":"+port, nil)
	checkError(err)
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
		checkError(err)
		err = ioutil.WriteFile(configPath, configBytes, 0777)
		checkError(err)
		return false
	}
	//Extract json from config
	data, err := ioutil.ReadFile(configPath)
	checkError(err)
	var dataJson map[string]interface{}
	err = json.Unmarshal(data, &dataJson)
	checkError(err)
	//Set maindata stuff
	maindata.Streamer = dataJson["streamer"].(string)
	maindata.CallbackURL = dataJson["callbackURL"].(string)
	maindata.Port = dataJson["port"].(string)
	return true
}

func getStreamerID(streamer string, token string, client http.Client) (id string) {
	//Form request for user data
	request, err := http.NewRequest("GET", "https://api.twitch.tv/helix/users?login="+streamer, nil)
	checkError(err)
	//Add auth header
	request.Header.Set("Client-ID", ClientID)
	request.Header.Set("Authorization", "Bearer "+token)

	//Get response
	response, err := client.Do(request)
	checkError(err)
	checkRateLimit(response)
	body, err := ioutil.ReadAll(response.Body)
	checkError(err)

	//Extract response to JSON
	var responseJson map[string]interface{}
	err = json.Unmarshal(body, &responseJson)
	_ = response.Body.Close()
	checkError(err)

	//Check user actually exists
	responseData := responseJson["data"]
	if len(responseData.([]interface{})) == 0 {
		log.Fatalln("Could not find streamer", streamer, ", aborting")
	}
	//Extract user ID
	firstData := responseData.([]interface{})[0].(map[string]interface{})
	id = firstData["id"].(string)
	return id
}

func getToken() string {
	response, err := http.Post("https://id.twitch.tv/oauth2/token"+
		"?client_id="+ClientID+
		"&client_secret="+secret.ClientSecret+
		"&grant_type=client_credentials", "application/json", nil)
	checkError(err)
	if response.StatusCode != 200 {
		log.Fatal("Failed to get token, status code:",response.StatusCode)
		return ""
	}
	var resultJson map[string]interface{}
	err = json.NewDecoder(response.Body).Decode(&resultJson)
	checkError(err)
	_ = response.Body.Close()
	token := resultJson["access_token"].(string)
	return token
}

func checkRateLimit(response *http.Response) {
	//TEMP: See rate limit
	rateLimit := response.Header.Get("Ratelimit-Limit")
	log.Println("Rate limit:", rateLimit)
	//Check remaining points
	remainingPoints, err := strconv.Atoi(response.Header.Get("Ratelimit-Remaining"))
	checkError(err)
	log.Println("Remaining points:", remainingPoints)
	if response.StatusCode == 429 {
		//Find reset time
		i, err := strconv.ParseInt(response.Header.Get("Ratelimit-Reset"), 10, 64)
		checkError(err)
		resetTime := time.Unix(i, 0)
		resetTimeString := resetTime.Format("3:04 PM")
		log.Fatalf("Twitch rate limit exceeded, cannot continue (Are you spamming?)\n"+
			"Rate limit will reset at: %s", resetTimeString)
	}
}

//Handles when the webhook issues a thingy
func handleHook(w http.ResponseWriter, r *http.Request) {
	//Respond to challenge query
	if r.Method == "GET" || r.Method == "" {
		query := r.URL.Query()
		if query["hub.mode"][0] == "denied" {
			_, err := fmt.Fprintf(w, "200 OK", nil)
			checkError(err)
			log.Println("Subscription to webhook was denied")
			return
		}
		if !checkRequest(query) {
			log.Println("Did not get same subscription back")
			return
		}
		challenge := query["hub.challenge"][0]
		log.Println("Challenge:",challenge)
		_, err := w.Write([]byte(challenge))
		checkError(err)
		return
	}
	//TODO: handle payload
}

//Checks that the request is what we requested
func checkRequest(values url.Values) bool {
	return values["hub.topic"][0] == TopicURL + maindata.ID
}