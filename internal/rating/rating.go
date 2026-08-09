// Package rating talks to the rating.chgk.info API. What this bot calls an
// Application, that API calls a "request", and the translation happens here
// and nowhere else.
package rating

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const DefaultAPI = "https://api.rating.chgk.net"

// betweenCalls keeps the bot from hammering a site run by volunteers.
const betweenCalls = 500 * time.Millisecond

type Client struct {
	BaseURL string
	// MinInterval is the politeness pause between calls.
	MinInterval time.Duration
	http        *http.Client

	mu       sync.Mutex
	lastCall time.Time
}

func New() *Client { return NewAt(DefaultAPI, betweenCalls) }

// NewAt points the client at another base URL, which is how the tests run it
// against a stand-in API.
func NewAt(baseURL string, minInterval time.Duration) *Client {
	return &Client{
		BaseURL:     baseURL,
		MinInterval: minInterval,
		http:        &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *Client) get(path string, into any) error {
	c.mu.Lock()
	if wait := c.MinInterval - time.Since(c.lastCall); wait > 0 {
		time.Sleep(wait)
	}
	c.lastCall = time.Now()
	c.mu.Unlock()

	resp, err := c.http.Get(c.BaseURL + path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("got response with error %d: %s", resp.StatusCode, body)
	}
	return json.Unmarshal(body, into)
}

// Info is a tournament as the rating site describes it.
type Info struct {
	ID              int    `json:"id"`
	Name            string `json:"name"`
	DateEnd         string `json:"dateEnd"`
	HideQuestionsTo string `json:"hideQuestionsTo"`
	// Detail carries the API's error text; "Not found" means the tournament
	// was deleted from the site.
	Detail string `json:"detail"`
}

// IsBad reports a tournament the site no longer has, which is the bot's cue to
// forget it.
func (i Info) IsBad() bool { return strings.EqualFold(i.Detail, "not found") }

func (c *Client) Info(tournamentID int) (Info, error) {
	var info Info
	err := c.get("/tournaments/"+strconv.Itoa(tournamentID)+".json", &info)
	return info, err
}

type apiApplication struct {
	ID             int    `json:"id"`
	Status         string `json:"status"`
	Representative struct {
		ID      int    `json:"id"`
		Name    string `json:"name"`
		Surname string `json:"surname"`
	} `json:"representative"`
	Venue struct {
		Town struct {
			Name string `json:"name"`
		} `json:"town"`
	} `json:"venue"`
}

// Application is one team asking to play, reduced to what the bot reports.
type Application struct {
	Status string `json:"status"`
	Rep    string `json:"rep"`
}

// Applications returns the tournament's unreviewed applications, keyed by id.
func (c *Client) Applications(tournamentID int) (map[string]Application, error) {
	var raw []apiApplication
	if err := c.get("/tournaments/"+strconv.Itoa(tournamentID)+"/requests.json?pagination=false", &raw); err != nil {
		return nil, err
	}
	out := make(map[string]Application, len(raw))
	for _, application := range raw {
		if application.Status != "N" {
			continue
		}
		rep := application.Representative
		out[strconv.Itoa(application.ID)] = Application{
			Status: application.Status,
			Rep: fmt.Sprintf("%d %s %s (%s)",
				rep.ID, rep.Name, rep.Surname, application.Venue.Town.Name),
		}
	}
	return out, nil
}

type Result struct {
	QuestionsTotal *int `json:"questionsTotal"`
	Current        struct {
		Name string `json:"name"`
		Town struct {
			Name string `json:"name"`
		} `json:"town"`
	} `json:"current"`
	Flags []struct {
		ShortName string `json:"shortName"`
	} `json:"flags"`
	TeamMembers    []TeamMember `json:"teamMembers"`
	Controversials []struct {
		Status string `json:"status"`
	} `json:"controversials"`
}

type TeamMember struct {
	Player struct {
		Name    string `json:"name"`
		Surname string `json:"surname"`
	} `json:"player"`
}

func (c *Client) Results(tournamentID int) ([]Result, error) {
	var results []Result
	err := c.get("/tournaments/"+strconv.Itoa(tournamentID)+
		"/results.json?pagination=false&includeTeamMembers=1"+
		"&includeTeamFlags=1&includeMasksAndControversials=1", &results)
	return results, err
}

type Appeal struct {
	Status string `json:"status"`
}

func (c *Client) Appeals(tournamentID int) ([]Appeal, error) {
	var appeals []Appeal
	err := c.get("/tournaments/"+strconv.Itoa(tournamentID)+"/appeals.json?pagination=false", &appeals)
	return appeals, err
}
