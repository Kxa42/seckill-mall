// 本文件承载 Gateway 到 Identity Service 的认证和地址 HTTP 适配。
package main

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"seckill-mall/api_gateway/middleware"
	"seckill-mall/common/pb"
	"seckill-mall/internal/platform/httpx"
)

func registerIdentityRoutes(router *gin.Engine, client pb.IdentityServiceClient) {
	if client == nil {
		return
	}
	router.POST("/api/v1/auth/register", func(c *gin.Context) {
		var request struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := c.ShouldBindJSON(&request); err != nil {
			httpx.Error(c, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数格式不正确")
			return
		}
		ctx, cancel := signedSystemContext(c, pb.IdentityService_Register_FullMethodName)
		defer cancel()
		response, err := client.Register(ctx, &pb.IdentityCredentialsRequest{Email: request.Email, Password: request.Password})
		if err != nil {
			respondStage4RPCError(c, err, "注册")
			return
		}
		httpx.OK(c, http.StatusCreated, response)
	})

	router.POST("/api/v1/auth/login", func(c *gin.Context) {
		var request struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := c.ShouldBindJSON(&request); err != nil {
			httpx.Error(c, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数格式不正确")
			return
		}
		ctx, cancel := signedSystemContext(c, pb.IdentityService_Login_FullMethodName)
		defer cancel()
		response, err := client.Login(ctx, &pb.IdentityCredentialsRequest{Email: request.Email, Password: request.Password})
		if err != nil {
			respondStage4RPCError(c, err, "登录")
			return
		}
		httpx.OK(c, http.StatusOK, response)
	})

	router.POST("/api/v1/auth/refresh", func(c *gin.Context) {
		var request struct {
			RefreshToken string `json:"refresh_token"`
		}
		if err := c.ShouldBindJSON(&request); err != nil {
			httpx.Error(c, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数格式不正确")
			return
		}
		ctx, cancel := signedSystemContext(c, pb.IdentityService_Refresh_FullMethodName)
		defer cancel()
		response, err := client.Refresh(ctx, &pb.IdentityRefreshRequest{RefreshToken: request.RefreshToken})
		if err != nil {
			respondStage4RPCError(c, err, "刷新令牌")
			return
		}
		httpx.OK(c, http.StatusOK, response)
	})

	authenticated := router.Group("/api/v1")
	authenticated.Use(middleware.CommerceJWTAuth())
	authenticated.GET("/addresses", func(c *gin.Context) {
		userID, ok := gatewayUserID(c)
		if !ok {
			return
		}
		ctx, cancel := signedUserContext(c, pb.IdentityService_ListAddresses_FullMethodName, userID)
		defer cancel()
		response, err := client.ListAddresses(ctx, &pb.IdentityAddressListRequest{UserId: userID})
		if err != nil {
			respondStage4RPCError(c, err, "地址")
			return
		}
		httpx.OK(c, http.StatusOK, response)
	})
	authenticated.POST("/addresses", func(c *gin.Context) {
		userID, ok := gatewayUserID(c)
		if !ok {
			return
		}
		var address pb.IdentityAddress
		if err := c.ShouldBindJSON(&address); err != nil {
			httpx.Error(c, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数格式不正确")
			return
		}
		ctx, cancel := signedUserContext(c, pb.IdentityService_CreateAddress_FullMethodName, userID)
		defer cancel()
		response, err := client.CreateAddress(ctx, &pb.IdentityAddressCreateRequest{UserId: userID, Address: addressRequest(&address)})
		if err != nil {
			respondStage4RPCError(c, err, "地址")
			return
		}
		httpx.OK(c, http.StatusCreated, response)
	})
	authenticated.PUT("/addresses/:address_id", func(c *gin.Context) {
		userID, ok := gatewayUserID(c)
		if !ok {
			return
		}
		addressID, err := strconv.ParseUint(c.Param("address_id"), 10, 64)
		if err != nil || addressID == 0 {
			httpx.Error(c, http.StatusBadRequest, "VALIDATION_ERROR", "地址 ID 无效")
			return
		}
		var address pb.IdentityAddress
		if err := c.ShouldBindJSON(&address); err != nil {
			httpx.Error(c, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数格式不正确")
			return
		}
		ctx, cancel := signedUserContext(c, pb.IdentityService_UpdateAddress_FullMethodName, userID)
		defer cancel()
		response, err := client.UpdateAddress(ctx, &pb.IdentityAddressUpdateRequest{UserId: userID, AddressId: addressID, Address: addressRequest(&address)})
		if err != nil {
			respondStage4RPCError(c, err, "地址")
			return
		}
		httpx.OK(c, http.StatusOK, response)
	})
	authenticated.DELETE("/addresses/:address_id", func(c *gin.Context) {
		userID, ok := gatewayUserID(c)
		if !ok {
			return
		}
		addressID, err := strconv.ParseUint(c.Param("address_id"), 10, 64)
		if err != nil || addressID == 0 {
			httpx.Error(c, http.StatusBadRequest, "VALIDATION_ERROR", "地址 ID 无效")
			return
		}
		ctx, cancel := signedUserContext(c, pb.IdentityService_DeleteAddress_FullMethodName, userID)
		defer cancel()
		response, err := client.DeleteAddress(ctx, &pb.IdentityAddressDeleteRequest{UserId: userID, AddressId: addressID})
		if err != nil {
			respondStage4RPCError(c, err, "地址")
			return
		}
		httpx.OK(c, http.StatusOK, response)
	})
}
