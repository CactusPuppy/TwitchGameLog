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
}

type StreamerData struct {
	Online bool
	Game string
	Title string
}

var Maindata MainData
var Streamerdata StreamerData
var Gamecache map[string]string

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
	Maindata.Token = token

	//Get config data
	if !getConfigData() {
		return
	}

	//Get game id cache

	streamer := Maindata.Streamer
	callbackURL := Maindata.CallbackURL
	port := Maindata.Port

	//Get streamer ID
	id := getStreamerID(streamer, token, client)
	Maindata.ID = id
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
	configPath := "config.yml"
	//Check if config exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		log.Println(`No config.yml file found, generating with defaults...
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
	Maindata.Streamer = dataJson["streamer"].(string)
	Maindata.CallbackURL = dataJson["callbackURL"].(string)
	Maindata.Port = dataJson["port"].(string)
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

//Get a game by its ID and put it into the cache
func getGameFromId(id string, client http.Client) (name string) {
	token := Maindata.Token
	if token == "" {
		log.Fatalln("No auth token found")
	}
	//Form request for user data
	request, err := http.NewRequest("GET", "https://api.twitch.tv/helix/games?id="+id, nil)
	checkError(err)
	//Add auth header
	request.Header.Set("Client-ID", ClientID)
	request.Header.Set("Authorization", "Bearer "+token)

	//Get response
	response, err := client.Do(request)
	checkError(err)
	if response.StatusCode != 200 {
		log.Fatalln("Error querying game from Twitch API, HTTP Code", response.StatusCode)
	}
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
		log.Println("Could not find game with ID", id)
		return ""
	}
	//Extract game name
	firstData := responseData.([]interface{})[0].(map[string]interface{})
	name = firstData["name"].(string)
	Gamecache[id] = name
	return name
}

//Saves the Gamecache to disk
func saveGameCache() {
	bytes, err := json.MarshalIndent(&Gamecache, "", "    ")
	checkError(err)

}

//Loads the Gamecache from disk
func loadGameCache() {

}

//Handles when the webhook issues a payload
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
		_, err := w.Write([]byte(challenge))
		checkError(err)
		return
	}
	//TODO: handle payload
	payload := make(map[string]interface{})
	err := json.NewDecoder(r.Body).Decode(&payload)
	checkErrorResponse(err, w)
	err = r.Body.Close()
	checkErrorResponse(err, w)
	fmt.Println("Got json payload: ")
	data := payload["data"].([]interface{})
	if len(data) == 0 {
		//Went offline
		Streamerdata.Online = false
		log.Println(Maindata.Streamer, "went offline")
		return
	}
	streamdata := data[0].(map[string]interface{})
	Streamerdata.Title = streamdata["title"].(string)
	if !Streamerdata.Online {
		Streamerdata.Online = true
		log.Println(Maindata.Streamer, "went online!")
	}
}

func checkErrorResponse(err error, w http.ResponseWriter) {
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

//Checks that the request is what we requested
func checkRequest(values url.Values) bool {
	return values["hub.topic"][0] == TopicURL + Maindata.ID
}