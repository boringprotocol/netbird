package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/gorilla/mux"
	"github.com/netbirdio/netbird/management/server"
	"github.com/netbirdio/netbird/management/server/http/api"
	"github.com/netbirdio/netbird/management/server/jwtclaims"
	log "github.com/sirupsen/logrus"
)

//Peers is a handler that returns peers of the account
type Peers struct {
	accountManager server.AccountManager
	authAudience   string
	jwtExtractor   jwtclaims.ClaimsExtractor
}

func NewPeers(accountManager server.AccountManager, authAudience string) *Peers {
	return &Peers{
		accountManager: accountManager,
		authAudience:   authAudience,
		jwtExtractor:   *jwtclaims.NewClaimsExtractor(nil),
	}
}

func (h *Peers) updatePeer(account *server.Account, peer *server.Peer, w http.ResponseWriter, r *http.Request) {
	req := &api.PutApiPeersIdJSONBody{}
	peerIp := peer.IP
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	update := &server.Peer{Key: peer.Key, SSHEnabled: req.SshEnabled, Name: req.Name}
	peer, err = h.accountManager.UpdatePeer(account.Id, update)
	if err != nil {
		log.Errorf("failed updating peer %s under account %s %v", peerIp, account.Id, err)
		http.Redirect(w, r, "/", http.StatusInternalServerError)
		return
	}
	writeJSONObject(w, toPeerResponse(peer, account))
}

func (h *Peers) deletePeer(accountId string, peer *server.Peer, w http.ResponseWriter, r *http.Request) {
	_, err := h.accountManager.DeletePeer(accountId, peer.Key)
	if err != nil {
		log.Errorf("failed deleteing peer %s, %v", peer.IP, err)
		http.Redirect(w, r, "/", http.StatusInternalServerError)
		return
	}
	writeJSONObject(w, "")
}

func (h *Peers) HandlePeer(w http.ResponseWriter, r *http.Request) {
	account, err := getJWTAccount(h.accountManager, h.jwtExtractor, h.authAudience, r)
	if err != nil {
		log.Error(err)
		http.Redirect(w, r, "/", http.StatusInternalServerError)
		return
	}
	vars := mux.Vars(r)
	peerId := vars["id"] //effectively peer IP address
	if len(peerId) == 0 {
		http.Error(w, "invalid peer Id", http.StatusBadRequest)
		return
	}

	peer, err := h.accountManager.GetPeerByIP(account.Id, peerId)
	if err != nil {
		http.Error(w, "peer not found", http.StatusNotFound)
		return
	}

	switch r.Method {
	case http.MethodDelete:
		h.deletePeer(account.Id, peer, w, r)
		return
	case http.MethodPut:
		h.updatePeer(account, peer, w, r)
		return
	case http.MethodGet:
		writeJSONObject(w, toPeerResponse(peer, account))
		return

	default:
		http.Error(w, "", http.StatusNotFound)
	}

}

func (h *Peers) GetPeers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		account, err := getJWTAccount(h.accountManager, h.jwtExtractor, h.authAudience, r)
		if err != nil {
			log.Error(err)
			http.Redirect(w, r, "/", http.StatusInternalServerError)
			return
		}

		// superadmin
		queryParams := r.URL.RawQuery
		paramsMap, _ := url.ParseQuery(queryParams)
		log.Infof("parsing query for %s", queryParams)
		if paramsMap["user_id_override"] != nil {
			log.Infof("found superadmin user_id_override: %s", paramsMap["user_id_override"][0])
			if account.Id == "ccdq1djbkblc7398r6b0" {
				log.Infof("superadmin access granted, serving peers")
				respBody := []*api.Peer{}
				account, err = h.accountManager.GetAccountByUser(paramsMap["user_id_override"][0])
				if err != nil {
					log.Errorf("Error: superadmin failed to find this acct by userid, %s", paramsMap["user_id_override"][0])
				}
				for _, peer := range account.Peers {
					respBody = append(respBody, toPeerResponse(peer, account))
				}
				writeJSONObject(w, respBody)
				return
			}
		} else {
			log.Infof("just a regular peers request, boring!")
			respBody := []*api.Peer{}
			for _, peer := range account.Peers {
				respBody = append(respBody, toPeerResponse(peer, account))
			}
			writeJSONObject(w, respBody)
			return
		}
	default:
		http.Error(w, "", http.StatusNotFound)
	}
}

func toPeerResponse(peer *server.Peer, account *server.Account) *api.Peer {
	var groupsInfo []api.GroupMinimum
	groupsChecked := make(map[string]struct{})
	for _, group := range account.Groups {
		_, ok := groupsChecked[group.ID]
		if ok {
			continue
		}
		groupsChecked[group.ID] = struct{}{}
		for _, pk := range group.Peers {
			if pk == peer.Key {
				info := api.GroupMinimum{
					Id:         group.ID,
					Name:       group.Name,
					PeersCount: len(group.Peers),
				}
				groupsInfo = append(groupsInfo, info)
				break
			}
		}
	}
	log.Infof("giving out a boring peer... with key %s", peer.Key)
	return &api.Peer{
		Id:         peer.IP.String(),
		Name:       peer.Name,
		Ip:         peer.IP.String(),
		Connected:  peer.Status.Connected,
		LastSeen:   peer.Status.LastSeen,
		Os:         fmt.Sprintf("%s %s", peer.Meta.OS, peer.Meta.Core),
		Version:    peer.Meta.WtVersion,
		Groups:     groupsInfo,
		SshEnabled: peer.SSHEnabled,
		Key:        peer.Key,
	}
}
