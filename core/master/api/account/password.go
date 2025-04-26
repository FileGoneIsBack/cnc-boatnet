package accountapi

import (
    "encoding/json"
    "net/http"
    "strings"

    "api/core/database"
    "api/core/database/users"
    "api/core/master/sessions"
    "api/core/models/server"
)

type pwdReq struct {
    OldPassword string `json:"oldPassword"`
    NewPassword string `json:"newPassword"`
}

func init() {
    Route.NewSub(server.NewRoute("/password", func(w http.ResponseWriter, r *http.Request) {
        ok, session := sessions.IsLoggedIn(w, r)
        if !ok {
            http.Redirect(w, r, "/login", http.StatusTemporaryRedirect)
            return
        }

        switch strings.ToLower(r.Method) {
        case "post":
            var req pwdReq
            if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
                http.Error(w, "Invalid JSON", http.StatusBadRequest)
                return
            }
            if !users.IsKey(session.User, []byte(req.OldPassword)) {
                http.Error(w, "Old password is incorrect", http.StatusUnauthorized)
                return
            }

            newSalt := database.NewSalt(16)
            newHash := database.NewHash([]byte(req.NewPassword), newSalt)
            if err := users.Container.ChangePassword(session.User.ID, newHash, newSalt); err != nil {
                http.Error(w, "Failed to update password", http.StatusInternalServerError)
                return
            }

            w.Header().Set("Content-Type", "application/json")
            w.Write([]byte(`{"success":true}`))
        default:
            w.WriteHeader(http.StatusMethodNotAllowed)
            w.Write([]byte("Method not allowed"))
        }
    }))
}
