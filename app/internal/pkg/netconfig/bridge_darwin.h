#ifndef BRIDGE_DARWIN_H
#define BRIDGE_DARWIN_H

#include <CoreFoundation/CoreFoundation.h>
#include <SystemConfiguration/SystemConfiguration.h>

typedef struct {
    char service_id[128];
    char name[128];
    char bsd_name[64];
    char if_type[64];
} sc_service_info_t;

int sc_get_services(sc_service_info_t** out_services, int* out_count);
void sc_free_services(sc_service_info_t* services);

#endif // BRIDGE_DARWIN_H
