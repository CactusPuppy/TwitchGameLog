package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/cactuspuppy/twitchgamelog/secret"
	"io/ioutil"
	"log"
	"net/http"
	"strconv"
	"time"
)

const ClientID = `ari2vux13uqzdxek5b4r1vw2vg80ix`

func checkError(err error) {
	if err != nil {
		log.Fatalln(err)
	}
}

func main() {
	start := time.Now()
	fmt.Println("Starting TwitchGameLog v0.1 by CactusPuppy")

	//Get our http client
	client := http.Client{}

	//Get an access token
	response, err := http.Post("https://id.twitch.tv/oauth2/token" +
		"?client_id=" + ClientID +
		"&client_secret=" + secret.ClientSecret +
		"&grant_type=client_credentials", "application/json", nil)
	checkError(err)


	//TODO: Get streamer from config
	streamer := "a_seagull"

	//Form request
	request, err := http.NewRequest("GET", "https://api.twitch.tv/helix/users?login=" + streamer, nil)
	checkError(err)
	//Add auth header
	request.Header.Set("Client-ID", ClientID)
	request.Header.Set("Authorization", "Bearer " + secret.ClientSecret)
	//Get response
	response, err = client.Do(request)
	checkError(err)

	//See rate limit
	rateLimit := response.Header.Get("Ratelimit-Limit")
	fmt.Println("Rate limit:", rateLimit)
	//Check remaining points
	remainingPoints, err := strconv.Atoi(response.Header.Get("Ratelimit-Remaining"))
	checkError(err)
	fmt.Println("Remaining points:", remainingPoints)
	//START TEMP
	//Find reset time
	i, err := strconv.ParseInt(response.Header.Get("Ratelimit-Reset"), 10, 64)
	checkError(err)
	resetTime := time.Unix(i, 0)
	resetTimeString := resetTime.Format("3 04 PM")
	fmt.Printf("Rate limit will reset at: %s\n", resetTimeString)
	//END TEMP
	//if remainingPoints < 1 {
	//	//Find reset time
	//	i, err := strconv.ParseInt(response.Header.Get("Ratelimit-Reset"), 10, 64)
	//	checkError(err)
	//	resetTime := time.Unix(i, 0)
	//	resetTimeString := resetTime.Format("3 04 PM")
	//	log.Fatalf("Twitch rate limit exceeded, cannot continue (Are you spamming?)\n" +
	//		"Rate limit will reset at: %s", resetTimeString)
	//	return
	//}
	//Print out response
	body, err := ioutil.ReadAll(response.Body)
	checkError(err)
	log.Println(string(body))

	//Subscribe to proper webhook
	hookURL := `https://api.twitch.tv/helix/webhooks/hub` //TODO
	payload := map[string]interface{}{
		"hub": map[string]string{
			"callback":"http://69.197.135.130",
			"mode":"subscribe",
			"topic":"https://api.twitch.tv/helix/streams?user_id=XXXX",
			"secret":secret.ClientSecret,
		},
	}
	payloadBytes, err := json.Marshal(payload)
	checkError(err)
	request, err = http.NewRequest("POST", hookURL, bytes.NewBuffer(payloadBytes))
	checkError(err)
	request.Header.Set("Client-ID", ClientID)


	elapsed := time.Since(start)
	log.Printf("Startup complete, took %s\n", elapsed)
}

func handleHook() {
	//TODO Handle events from the hjook
}
