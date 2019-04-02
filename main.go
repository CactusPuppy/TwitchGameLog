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
	"strconv"
	"time"
)

const ClientID = `ari2vux13uqzdxek5b4r1vw2vg80ix`

type ConfigData struct {
	Streamer string
	CallbackURL string
	Port string
	ID string
}

var configdata ConfigData

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

	//TODO: Get config values
	configdata.Streamer = "cactuspupbot"
	configdata.CallbackURL = "https://480ba225.ngrok.io"
	configdata.Port = "8080"

	streamer := configdata.Streamer
	callbackURL := configdata.CallbackURL
	port := configdata.Port

	//Get streamer ID
	id := getStreamerID(streamer, token, client)
	configdata.ID = id
	log.Println("Now tracking",streamer,"(ID:",id+")")

	//Subscribe to proper webhook
	hookURL := `https://api.twitch.tv/helix/webhooks/hub` //TODO
	payload := map[string]interface{}{
		"hub.callback":      callbackURL+"/webhook",
		"hub.mode":          "subscribe",
		"hub.topic":         "https://api.twitch.tv/helix/streams?user_id=" + id,
		"hub.lease_seconds": "0",
		"hub.secret":        secret.PayloadSecret,
	}
	payloadBytes, err := json.Marshal(payload)
	checkError(err)

	//TEMP
	var unmarsh map[string]interface{}
	err = json.Unmarshal(payloadBytes, &unmarsh)

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
	fmt.Printf("%v\n", responseJson)

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
	if remainingPoints < 1 {
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
	//TODO: Respond to query
	if r.Method == "GET" || r.Method == "" {
		query := r.URL.Query()
		if query["hub.mode"][0] == "denied" {
			log.Fatalln("Subscription to webhook was denied")
		}
		if !checkRequest(query) {
			log.Fatalln("Did not get same subscription back")
		}

	}
	//TODO: handle payload
}

//Checks that the request is what we requested
func checkRequest(values url.Values) bool {

}