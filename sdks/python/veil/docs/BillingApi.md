# veil.BillingApi

All URIs are relative to *https://veil.nyc*

Method | HTTP request | Description
------------- | ------------- | -------------
[**billing_webhook**](BillingApi.md#billing_webhook) | **POST** /v1/billing/webhook | Vortex billing webhook receiver. Auth is the Vortex-Signature header (HMAC-SHA256 over the raw body) — no org principal. Subscription and entitlement lifecycle events flip the org&#39;s plan state; unknown event types are durable no-ops. Not MCP.
[**get_billing**](BillingApi.md#get_billing) | **GET** /v1/billing | Owner-facing billing state: enforced plan plus this window&#39;s use count against the free allowance. included is null when metering is off or the plan is paid. The vault&#39;s cap banner reads this. Not MCP.


# **billing_webhook**
> billing_webhook(vortex_signature, request_body)

Vortex billing webhook receiver. Auth is the Vortex-Signature header (HMAC-SHA256 over the raw body) — no org principal. Subscription and entitlement lifecycle events flip the org's plan state; unknown event types are durable no-ops. Not MCP.

### Example


```python
import veil
from veil.rest import ApiException
from pprint import pprint

# Defining the host is optional and defaults to https://veil.nyc
# See configuration.py for a list of all supported configuration parameters.
configuration = veil.Configuration(
    host = "https://veil.nyc"
)


# Enter a context with an instance of the API client
with veil.ApiClient(configuration) as api_client:
    # Create an instance of the API class
    api_instance = veil.BillingApi(api_client)
    vortex_signature = 'vortex_signature_example' # str | t=<ms>,v1=<hex-hmac>
    request_body = None # Dict[str, object] | 

    try:
        # Vortex billing webhook receiver. Auth is the Vortex-Signature header (HMAC-SHA256 over the raw body) — no org principal. Subscription and entitlement lifecycle events flip the org's plan state; unknown event types are durable no-ops. Not MCP.
        api_instance.billing_webhook(vortex_signature, request_body)
    except Exception as e:
        print("Exception when calling BillingApi->billing_webhook: %s\n" % e)
```



### Parameters


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **vortex_signature** | **str**| t&#x3D;&lt;ms&gt;,v1&#x3D;&lt;hex-hmac&gt; | 
 **request_body** | [**Dict[str, object]**](object.md)|  | 

### Return type

void (empty response body)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: Not defined

### HTTP response details

| Status code | Description | Response headers |
|-------------|-------------|------------------|
**204** | accepted (or durable no-op) |  -  |
**400** | malformed event body |  -  |
**401** | missing or invalid signature |  -  |
**404** | billing webhook not configured |  -  |
**413** | body too large |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **get_billing**
> BillingView get_billing()

Owner-facing billing state: enforced plan plus this window's use count against the free allowance. included is null when metering is off or the plan is paid. The vault's cap banner reads this. Not MCP.

### Example

* Bearer (JWT) Authentication (bearerAuth):

```python
import veil
from veil.models.billing_view import BillingView
from veil.rest import ApiException
from pprint import pprint

# Defining the host is optional and defaults to https://veil.nyc
# See configuration.py for a list of all supported configuration parameters.
configuration = veil.Configuration(
    host = "https://veil.nyc"
)

# The client must configure the authentication and authorization parameters
# in accordance with the API server security policy.
# Examples for each auth method are provided below, use the example that
# satisfies your auth use case.

# Configure Bearer authorization (JWT): bearerAuth
configuration = veil.Configuration(
    access_token = os.environ["BEARER_TOKEN"]
)

# Enter a context with an instance of the API client
with veil.ApiClient(configuration) as api_client:
    # Create an instance of the API class
    api_instance = veil.BillingApi(api_client)

    try:
        # Owner-facing billing state: enforced plan plus this window's use count against the free allowance. included is null when metering is off or the plan is paid. The vault's cap banner reads this. Not MCP.
        api_response = api_instance.get_billing()
        print("The response of BillingApi->get_billing:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling BillingApi->get_billing: %s\n" % e)
```



### Parameters

This endpoint does not need any parameter.

### Return type

[**BillingView**](BillingView.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

### HTTP response details

| Status code | Description | Response headers |
|-------------|-------------|------------------|
**200** | Billing state |  -  |
**401** | missing or invalid Bearer |  -  |
**403** | not owner |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

