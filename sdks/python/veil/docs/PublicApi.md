# veil.PublicApi

All URIs are relative to *https://veil.nyc*

Method | HTTP request | Description
------------- | ------------- | -------------
[**redeem_share**](PublicApi.md#redeem_share) | **POST** /v1/shares/{id}/redeem | Redeem a share link. Public — the link id is the capability. Returns the sealed envelope exactly once; consumed, revoked, expired, and missing all read a generic 404. Per-id rate limited. Audit share_redeemed.


# **redeem_share**
> RedeemShareResponse redeem_share(id)

Redeem a share link. Public — the link id is the capability. Returns the sealed envelope exactly once; consumed, revoked, expired, and missing all read a generic 404. Per-id rate limited. Audit share_redeemed.

### Example


```python
import veil
from veil.models.redeem_share_response import RedeemShareResponse
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
    api_instance = veil.PublicApi(api_client)
    id = 'id_example' # str | 

    try:
        # Redeem a share link. Public — the link id is the capability. Returns the sealed envelope exactly once; consumed, revoked, expired, and missing all read a generic 404. Per-id rate limited. Audit share_redeemed.
        api_response = api_instance.redeem_share(id)
        print("The response of PublicApi->redeem_share:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling PublicApi->redeem_share: %s\n" % e)
```



### Parameters


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **id** | **str**|  | 

### Return type

[**RedeemShareResponse**](RedeemShareResponse.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

### HTTP response details

| Status code | Description | Response headers |
|-------------|-------------|------------------|
**200** | The sealed envelope |  -  |
**404** | missing, consumed, revoked, or expired |  -  |
**429** | redeem rate limit |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

