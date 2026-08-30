#include "bridge_darwin.h"
#include <stdlib.h>
#include <string.h>

// 获取当前 active set 的所有 Network Services
int sc_get_services(sc_service_info_t** out_services, int* out_count) {
    if (!out_services || !out_count) return -1;
    *out_services = NULL;
    *out_count = 0;

    SCPreferencesRef prefs = SCPreferencesCreate(NULL, CFSTR("aivision-network"), NULL);
    if (!prefs) return -2;

    SCNetworkSetRef currentSet = SCNetworkSetCopyCurrent(prefs);
    if (!currentSet) {
        CFRelease(prefs);
        return -3;
    }

    CFArrayRef services = SCNetworkSetCopyServices(currentSet);
    if (!services) {
        CFRelease(currentSet);
        CFRelease(prefs);
        return 0; // 无 service
    }

    CFIndex count = CFArrayGetCount(services);
    if (count == 0) {
        CFRelease(services);
        CFRelease(currentSet);
        CFRelease(prefs);
        return 0;
    }

    sc_service_info_t* list = (sc_service_info_t*)calloc(count, sizeof(sc_service_info_t));
    if (!list) {
        CFRelease(services);
        CFRelease(currentSet);
        CFRelease(prefs);
        return -4;
    }

    int valid_count = 0;
    for (CFIndex i = 0; i < count; i++) {
        SCNetworkServiceRef service = (SCNetworkServiceRef)CFArrayGetValueAtIndex(services, i);
        if (!service) continue;

        CFStringRef serviceID = SCNetworkServiceGetServiceID(service);
        CFStringRef name = SCNetworkServiceGetName(service);
        SCNetworkInterfaceRef iface = SCNetworkServiceGetInterface(service);

        if (!serviceID || !iface) continue;

        CFStringRef bsdName = SCNetworkInterfaceGetBSDName(iface);
        CFStringRef ifType = SCNetworkInterfaceGetInterfaceType(iface);

        if (serviceID) {
            CFStringGetCString(serviceID, list[valid_count].service_id, sizeof(list[valid_count].service_id), kCFStringEncodingUTF8);
        }
        if (name) {
            CFStringGetCString(name, list[valid_count].name, sizeof(list[valid_count].name), kCFStringEncodingUTF8);
        }
        if (bsdName) {
            CFStringGetCString(bsdName, list[valid_count].bsd_name, sizeof(list[valid_count].bsd_name), kCFStringEncodingUTF8);
        }
        if (ifType) {
            CFStringGetCString(ifType, list[valid_count].if_type, sizeof(list[valid_count].if_type), kCFStringEncodingUTF8);
        }

        valid_count++;
    }

    CFRelease(services);
    CFRelease(currentSet);
    CFRelease(prefs);

    *out_services = list;
    *out_count = valid_count;
    return 0;
}

void sc_free_services(sc_service_info_t* services) {
    if (services) {
        free(services);
    }
}
