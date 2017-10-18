package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"time"

	"golang.org/x/oauth2"
)

var (
	oauthConfig = &oauth2.Config{
		RedirectURL:  "https://kontoret.pixpro.net:8080/callback",
		ClientID:     os.Getenv("OFFICE_CLIENT_ID"),
		ClientSecret: os.Getenv("OFFICE_CLIENT_SECRET"),
		Scopes:       []string{"Mail.Read", "Mail.Read.Shared", "openid", "offline_access"},
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://login.microsoftonline.com/common/oauth2/v2.0/authorize",
			TokenURL: "https://login.microsoftonline.com/common/oauth2/v2.0/token",
		},
	}
)

const htmlIndex = `<html><body>
<a href="/login">Log in</a>
</body></html>
`
const filename = "config.json"

func main() {
	http.HandleFunc("/", handleMain)
	http.HandleFunc("/login", handleLogin)
	http.HandleFunc("/callback", handleCallback)
	http.HandleFunc("/webhook", handleWebhook)

	go func() {
		log.Println(http.ListenAndServeTLS(":8080", "kontoret.pixpro.net.crt", "kontoret.pixpro.net.key", nil))
	}()

	token := &oauth2.Token{}
	if err := loadJson(filename, token); err == nil {
		if !token.Valid() {
			token, err = oauthConfig.TokenSource(context.Background(), token).Token()
			if err != nil {
				log.Println(err)
			}
			if err := saveJson(filename, token); err != nil {
				log.Panicln(err)
			}
		}
		client := getClient(context.Background(), token)

		setUpWebhook(client)
		runner(client)
	}

	fmt.Println("Web is now running.  Press CTRL-C to exit.")
	// Simple way to keep program running until CTRL-C is pressed.
	<-make(chan struct{})
}

func saveJson(name string, i interface{}) error {
	data, err := json.Marshal(i)
	if err != nil {
		return err
	}
	if err := ioutil.WriteFile(name, data, 0664); err != nil {
		return err
	}
	return nil
}

func loadJson(name string, i interface{}) error {
	data, err := ioutil.ReadFile(name)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, &i); err != nil {
		return err
	}
	return nil
}

func setUpWebhook(client *http.Client) {
	sr := Subscription{}
	//sr.Resource = "/me/mailfolders('inbox')/messages"
	sr.Resource = "me/messages"
	sr.NotificationURL = "https://kontoret.pixpro.net:8080/webhook"
	sr.ChangeType = "created, deleted, updated"
	sr.ExpirationDateTime = time.Now().Add(30 * time.Minute)

	b := new(bytes.Buffer)
	err := json.NewEncoder(b).Encode(sr)
	if err != nil {
		log.Println("1", err)
		return
	}
	resp, err := client.Post("https://graph.microsoft.com/v1.0/subscriptions", "application/json", b)
	if err != nil {
		log.Println("2", err)
		return
	}
	defer resp.Body.Close()
	fmt.Println(resp.StatusCode, resp.Status)
	//fmt.Println(jsonFormater.GetFromReader(resp.Body))
	//return
	r := Subscription{}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		log.Println("3", err)
		//return
	}
	saveJson("resp.json", r)
}

func handleWebhook(w http.ResponseWriter, r *http.Request) {
	log.Println("webhook")

	id := r.FormValue("validationToken")
	if id != "" {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintf(w, id)
		return
	}

	defer r.Body.Close()
	/*b, err := ioutil.ReadAll(r.Body)
	if err != nil {
		fmt.Println(err)
	}
	fmt.Println("resp = ", string(b))*/
	web := &WebHookReponse{}
	if err := json.NewDecoder(r.Body).Decode(web); err != nil {
		log.Println(err)
	}
	saveJson("wbs.json", web)
	w.WriteHeader(http.StatusAccepted)
}

func handleMain(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, htmlIndex)
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	url := oauthConfig.AuthCodeURL("")
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

func handleCallback(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	code := r.FormValue("code")

	httpClient := &http.Client{Timeout: 1 * time.Minute}
	ctx = context.WithValue(ctx, oauth2.HTTPClient, httpClient)
	token, err := oauthConfig.Exchange(ctx, code)
	if err != nil {
		log.Println("Exchange err", err)
		return
	}
	http.Redirect(w, r, "https://kontoret.pixpro.net:8080", http.StatusTemporaryRedirect)

	if err := saveJson(filename, token); err != nil {
		log.Println(err)
	}
	client := getClient(ctx, token)
	runner(client)
}

func runner(client *http.Client) {
	if err := listMailBox(client); err != nil {
		log.Println(err)
	}
	ticker := time.NewTicker(10 * time.Minute)
	quit := make(chan struct{})
	go func() {
		for {
			select {
			case <-ticker.C:
				if err := listMailBox(client); err != nil {
					log.Println(err)
				}
			case <-quit:
				ticker.Stop()
				return
			}
		}
	}()
}

func listMailBox(client *http.Client) error {
	//resp, err := client.Get("https://graph.microsoft.com/v1.0/me/mailfolders/inbox/messages")
	resp, err := client.Get("https://graph.microsoft.com/v1.0/me/messages")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	mb := Mailbox{}
	if err := json.NewDecoder(resp.Body).Decode(&mb); err != nil {
		return err
	}
	for _, row := range mb.Value {
		fmt.Println(row.Subject)
		//fmt.Println(row.Body)

	}
	fmt.Println("--------------------")
	return nil
}

func getClient(ctx context.Context, t *oauth2.Token) *http.Client {
	log.Println("Token expiry", t.Expiry.String())
	client := oauthConfig.Client(ctx, t)
	client.Timeout = 2 * time.Minute
	return client
}

type Subscription struct {
	ChangeType         string    `json:"changeType"`
	ClientState        string    `json:"clientState"`
	ExpirationDateTime time.Time `json:"expirationDateTime"`
	ID                 string    `json:"id"`
	NotificationURL    string    `json:"notificationUrl"`
	Resource           string    `json:"resource"`
}

type WebHookReponse struct {
	Value []struct {
		SubscriptionID                 string    `json:"subscriptionId"`
		SubscriptionExpirationDateTime time.Time `json:"subscriptionExpirationDateTime"`
		ChangeType                     string    `json:"changeType"`
		Resource                       string    `json:"resource"`
		ResourceData                   struct {
			OdataType string `json:"@odata.type"`
			OdataID   string `json:"@odata.id"`
			OdataEtag string `json:"@odata.etag"`
			ID        string `json:"id"`
		} `json:"resourceData"`
		ClientState string `json:"clientState"`
	} `json:"value"`
}

type Mailbox struct {
	_odata_context string `json:"@odata.context"`
	Value          []struct {
		_odata_etag   string        `json:"@odata.etag"`
		BccRecipients []interface{} `json:"bccRecipients"`
		Body          struct {
			Content     string `json:"content"`
			ContentType string `json:"contentType"`
		} `json:"body"`
		BodyPreview     string        `json:"bodyPreview"`
		Categories      []interface{} `json:"categories"`
		CcRecipients    []interface{} `json:"ccRecipients"`
		ChangeKey       string        `json:"changeKey"`
		ConversationID  string        `json:"conversationId"`
		CreatedDateTime string        `json:"createdDateTime"`
		From            struct {
			EmailAddress struct {
				Address string `json:"address"`
				Name    string `json:"name"`
			} `json:"emailAddress"`
		} `json:"from"`
		HasAttachments             bool          `json:"hasAttachments"`
		ID                         string        `json:"id"`
		Importance                 string        `json:"importance"`
		InferenceClassification    string        `json:"inferenceClassification"`
		InternetMessageID          string        `json:"internetMessageId"`
		IsDeliveryReceiptRequested interface{}   `json:"isDeliveryReceiptRequested"`
		IsDraft                    bool          `json:"isDraft"`
		IsRead                     bool          `json:"isRead"`
		IsReadReceiptRequested     bool          `json:"isReadReceiptRequested"`
		LastModifiedDateTime       string        `json:"lastModifiedDateTime"`
		ParentFolderID             string        `json:"parentFolderId"`
		ReceivedDateTime           string        `json:"receivedDateTime"`
		ReplyTo                    []interface{} `json:"replyTo"`
		Sender                     struct {
			EmailAddress struct {
				Address string `json:"address"`
				Name    string `json:"name"`
			} `json:"emailAddress"`
		} `json:"sender"`
		SentDateTime string `json:"sentDateTime"`
		Subject      string `json:"subject"`
		ToRecipients []struct {
			EmailAddress struct {
				Address string `json:"address"`
				Name    string `json:"name"`
			} `json:"emailAddress"`
		} `json:"toRecipients"`
		WebLink string `json:"webLink"`
	} `json:"value"`
}
