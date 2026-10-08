# 支付宝支付调用示例

本文档说明如何使用 `pkg/alipayx` 接入支付宝支付、同步回调验签、异步通知和退款。

## 配置

```yaml
alipay:
  app_id: ""
  private_key: ""
  alipay_public_key: ""
  production: false
  notify_url: "https://api.example.com/payments/alipay/notify"
  return_url: "https://www.example.com/orders/alipay/return"
  encrypt_key: ""
  app_cert_public_key_path: ""
  alipay_root_cert_path: ""
  alipay_cert_public_key_path: ""
```

普通公钥模式和证书模式二选一。普通公钥模式配置 `alipay_public_key`；证书模式配置三个证书路径。

## 初始化

```go
if err := config.InitGlobal("configs/config.yaml"); err != nil {
    panic(err)
}
cfg := config.MustGlobal()

payClient, err := alipayx.NewClient(cfg.Alipay)
if err != nil {
    panic(err)
}

orderSvc := orderservice.NewService(repo, payClient, log)
```

推荐在入口初始化一次，然后注入业务 service：

```text
handler -> service -> alipayx.Client
```

## 创建支付

实际业务 service 中直接调用 `pkg/alipayx`，不要在业务项目里再复制一层支付宝工具类。

```go
func (s *Service) CreateAlipayPagePayment(ctx context.Context, orderID string) (string, error) {
    order, err := s.repo.FindByID(ctx, orderID)
    if err != nil {
        return "", err
    }
    if order.PaidAt != nil {
        return "", apperrors.FailedPrecondition("ORDER_ALREADY_PAID", "order already paid")
    }

    return s.alipay.PagePayURL(alipayx.PayRequest{
        OutTradeNo:     order.PayNo,
        Subject:        "订单支付",
        TotalAmount:    order.PayAmount.StringFixed(2),
        TimeoutExpress: "15m",
    })
}
```

Gin handler 只处理协议入参和出参：

```go
func CreateAlipayPagePayment(svc *orderservice.Service) gin.HandlerFunc {
    return func(c *gin.Context) {
        var req struct {
            OrderID string `json:"order_id" binding:"required"`
        }
        if err := c.ShouldBindJSON(&req); err != nil {
            httpx.Error(c, apperrors.InvalidArgument("INVALID_REQUEST", err.Error()))
            return
        }

        payURL, err := svc.CreateAlipayPagePayment(c.Request.Context(), req.OrderID)
        if err != nil {
            httpx.Error(c, err)
            return
        }
        httpx.OK(c, gin.H{"pay_url": payURL})
    }
}
```

## 同步回调验签

```go
func AlipayReturn(payClient *alipayx.Client) gin.HandlerFunc {
    return func(c *gin.Context) {
        if err := c.Request.ParseForm(); err != nil {
            httpx.Error(c, apperrors.InvalidArgument("INVALID_REQUEST", err.Error()))
            return
        }
        if err := payClient.VerifyReturn(c.Request.Context(), c.Request.Form); err != nil {
            httpx.Error(c, apperrors.InvalidArgument("ALIPAY_SIGN_INVALID", err.Error()))
            return
        }

        httpx.OK(c, gin.H{
            "out_trade_no": c.Request.Form.Get("out_trade_no"),
            "trade_no":     c.Request.Form.Get("trade_no"),
        })
    }
}
```

## 异步通知回调

支付宝异步通知必须验签、校验订单号和金额、幂等更新订单状态。处理成功后返回纯文本 `success`。

```go
func AlipayNotify(payClient *alipayx.Client, svc *orderservice.Service) gin.HandlerFunc {
    return func(c *gin.Context) {
        if err := c.Request.ParseForm(); err != nil {
            c.String(http.StatusBadRequest, "fail")
            return
        }

        notification, err := payClient.DecodeNotification(c.Request.Context(), c.Request.Form)
        if err != nil {
            c.String(http.StatusBadRequest, "fail")
            return
        }

        err = svc.MarkAlipayPaid(
            c.Request.Context(),
            notification.OutTradeNo,
            notification.TradeNo,
            notification.TotalAmount,
            notification.TradeStatus,
        )
        if err != nil {
            c.String(http.StatusInternalServerError, "fail")
            return
        }

        c.String(http.StatusOK, "success")
    }
}
```

## 退款

```go
func (s *Service) RefundAlipayOrder(ctx context.Context, orderID string, reason string) error {
    order, err := s.repo.FindByID(ctx, orderID)
    if err != nil {
        return err
    }
    if order.RefundedAt != nil {
        return nil
    }

    return s.alipay.RefundOK(ctx, alipayx.RefundRequest{
        OutTradeNo:   order.PayNo,
        RefundAmount: order.PayAmount.StringFixed(2),
        RefundReason: reason,
        OutRequestNo: order.PayNo + "-refund-001",
    })
}
```
