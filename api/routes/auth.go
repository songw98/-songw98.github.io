package routes

import (
	"errors"
	"net/http"
	"strconv"

	jwt "github.com/golang-jwt/jwt"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/mitchellh/mapstructure"
	"github.com/shellhub-io/shellhub/api/pkg/gateway"
	errs "github.com/shellhub-io/shellhub/api/routes/errors"
	svc "github.com/shellhub-io/shellhub/api/services"
	client "github.com/shellhub-io/shellhub/pkg/api/internalclient"
	"github.com/shellhub-io/shellhub/pkg/api/requests"
	"github.com/shellhub-io/shellhub/pkg/models"
)

const (
	AuthRequestURL  = "/auth"
	AuthDeviceURL   = "/devices/auth"
	AuthDeviceURLV2 = "/auth/device"
	AuthUserURL     = "/login"
	AuthUserURLV2   = "/auth/user"

	AuthUserTokenInternalURL = "/auth/token/:id"     //nolint:gosec
	AuthUserTokenPublicURL   = "/auth/token/:tenant" //nolint:gosec

	AuthPublicKeyURL = "/auth/ssh"
	AuthMFAURL       = "/auth/mfa"
)

const (
	// AuthRequestUserToken is the type of the token used to authenticate a user.
	AuthRequestUserToken = "user"
	// AuthRequestDeviceToken is the type of the token used to authenticate a device.
	AuthRequestDeviceToken = "device"
)

// AuthRequest checks the user and device authentication token.
//
// This route is a special route and it is called every time a user tries to access a route which requires
// authentication. It gets the JWT token sent, unwraps it and sets the information, like tenant, user, etc., as headers
// of the response to be got in the subsequent through the [gateway.Context].
func (h *Handler) AuthRequest(c gateway.Context) error {
	token, ok := c.Get(middleware.DefaultJWTConfig.ContextKey).(*jwt.Token)
	if !ok {
		return svc.ErrTypeAssertion
	}

	rawClaims, ok := token.Claims.(*jwt.MapClaims)
	if !ok {
		return svc.ErrTypeAssertion
	}

	// setHeader sets a reader to the HTTP response to be read in the subsequent request.
	setHeader := func(response gateway.Context, key string, value string) {
		response.Response().Header().Set(key, value)
	}

	// decodeMap parses the JWT claims into a struct.
	decodeMap := func(input *jwt.MapClaims, output any) error {
		config := &mapstructure.DecoderConfig{
			TagName:  "json",
			Metadata: nil,
			Result:   output,
		}

		decoder, err := mapstructure.NewDecoder(config)
		if err != nil {
			return err
		}

		return decoder.Decode(input)
	}

	switch claims := (*rawClaims)["claims"]; claims {
	case AuthRequestUserToken:
		// A [AuthRequestUserToken] is a token used to authenticate a user.
		// This kind of token can have its "namespace" as a empty value, indicating that is a "user" token. Its a kind
		// of sub-token, what allows the logged user to change its information, but does not allow to change the any
		// other namespace information.

		var claims models.UserAuthClaims
		if err := decodeMap(rawClaims, &claims); err != nil {
			return err
		}

		args := c.QueryParam("args")
		if args != "skip" && claims.Tenant != "" {
			// This forces any no cached token to be invalid, even if it not not expired.
			if ok, err := h.service.AuthIsCacheToken(c.Ctx(), claims.Tenant, claims.ID); err != nil || !ok {
				return svc.NewErrAuthUnathorized(err)
			}
		}

		MFA, err := h.service.AuthMFA(c.Ctx(), claims.ID)
		if err != nil {
			return err
		}

		if MFA != claims.MFA.Status {
			if !MFA {
				return svc.NewErrAuthUnathorized(errors.New("necessary enable MFA"))
			}
		}
		// Extract datas of user from JWT
		c.Response().Header().Set("X-Tenant-ID", claims.Tenant)
		c.Response().Header().Set("X-Username", claims.Username)
		c.Response().Header().Set("X-ID", claims.ID)
		c.Response().Header().Set("X-Role", claims.Role)
		c.Response().Header().Set("X-MFA", strconv.FormatBool(claims.MFA.Status))
		c.Response().Header().Set("X-Validate-MFA", strconv.FormatBool(claims.MFA.Validate))

		return c.NoContent(http.StatusOK)
	case AuthRequestDeviceToken:
		var claims models.DeviceAuthClaims

		if err := decodeMap(rawClaims, &claims); err != nil {
			return err
		}

		// Extract device UID from JWT and set it into the header.
		setHeader(c, client.DeviceUIDHeader, claims.UID)

		return c.NoContent(http.StatusOK)
	default:

		return svc.NewErrAuthUnathorized(nil)
	}
}

func (h *Handler) AuthDevice(c gateway.Context) error {
	var req requests.DeviceAuth
	if err := c.Bind(&req); err != nil {
		return err
	}

	if err := c.Validate(&req); err != nil {
		return err
	}

	ip := c.Request().Header.Get("X-Real-IP")
	res, err := h.service.AuthDevice(c.Ctx(), req, ip)
	if err != nil {
		return err
	}

	err = h.service.SetDevicePosition(c.Ctx(), models.UID(res.UID), ip)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, res)
}

func (h *Handler) AuthUser(c gateway.Context) error {
	var req requests.UserAuth
	if err := c.Bind(&req); err != nil {
		return err
	}

	if err := c.Validate(&req); err != nil {
		return err
	}

	res, err := h.service.AuthUser(c.Ctx(), &models.UserAuthRequest{
		Identifier: models.UserAuthIdentifier(req.Username),
		Password:   req.Password,
	}, true)
	if err != nil {
		if errors.Is(err, svc.ErrUserNotFound) {
			return errs.NewErrUnauthorized(err)
		}

		return err
	}

	return c.JSON(http.StatusOK, res)
}

func (h *Handler) AuthUserInfo(c gateway.Context) error {
	username := c.Request().Header.Get("X-Username")
	tenant := c.Request().Header.Get("X-Tenant-ID")
	token := c.Request().Header.Get(echo.HeaderAuthorization)

	res, err := h.service.AuthUserInfo(c.Ctx(), username, tenant, token)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, res)
}

func (h *Handler) AuthGetToken(c gateway.Context) error {
	var req requests.AuthTokenGet

	if err := c.Bind(&req); err != nil {
		return err
	}

	if err := c.Validate(&req); err != nil {
		return err
	}

	res, err := h.service.AuthGetToken(c.Ctx(), req.ID, req.MFA)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, res)
}

func (h *Handler) AuthSwapToken(c gateway.Context) error {
	var req requests.AuthTokenSwap
	if err := c.Bind(&req); err != nil {
		return err
	}

	if err := c.Validate(&req); err != nil {
		return err
	}
	var id string
	if v := c.ID(); v != nil {
		id = v.ID
	}

	res, err := h.service.AuthSwapToken(c.Ctx(), id, req.Tenant)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, res)
}

func (h *Handler) AuthPublicKey(c gateway.Context) error {
	var req requests.PublicKeyAuth
	if err := c.Bind(&req); err != nil {
		return err
	}

	if err := c.Validate(&req); err != nil {
		return err
	}

	res, err := h.service.AuthPublicKey(c.Ctx(), req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, res)
}

func AuthMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx, ok := c.Get("ctx").(*gateway.Context)
		if !ok {
			return svc.ErrTypeAssertion
		}
		jwt := middleware.JWTWithConfig(middleware.JWTConfig{ //nolint:staticcheck
			Claims:        &jwt.MapClaims{},
			SigningKey:    ctx.Service().(svc.Service).PublicKey(),
			SigningMethod: "RS256",
		})

		return jwt(next)(c)
	}
}

func AuthMiddlewareMFA(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		statusMFA := c.Request().Header.Get("X-MFA")
		validateMFA := c.Request().Header.Get("X-Validate-MFA")

		exemptedPaths := []string{"/api/mfa/generate", "/api/mfa/enable", "/api/mfa/desable", "/api/mfa/recovery", "/api/mfa/auth"}
		currentPath := c.Path()
		isExempted := false

		for _, path := range exemptedPaths {
			if currentPath == path {
				isExempted = true
			}
		}

		if !isExempted && statusMFA != "" {
			status, err := strconv.ParseBool(statusMFA)
			if err != nil {
				return err
			}

			if status {
				validate, err := strconv.ParseBool(validateMFA)
				if err != nil {
					return err
				}

				if !validate {
					return svc.NewErrAuthUnathorized(nil)
				}
			}
		}

		return (next)(c)
	}
}
