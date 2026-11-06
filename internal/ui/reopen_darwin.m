#import <Cocoa/Cocoa.h>

extern void lanbridgeReopen(void);

@interface LBReopenHandler : NSObject
- (void)handleReopen:(NSAppleEventDescriptor *)event withReply:(NSAppleEventDescriptor *)reply;
@end
@implementation LBReopenHandler
- (void)handleReopen:(NSAppleEventDescriptor *)event withReply:(NSAppleEventDescriptor *)reply {
	lanbridgeReopen();
}
@end

static LBReopenHandler *lbHandler;

void lanbridgeInstallReopen(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		lbHandler = [LBReopenHandler new];
		[[NSAppleEventManager sharedAppleEventManager]
			setEventHandler:lbHandler
			andSelector:@selector(handleReopen:withReply:)
			forEventClass:kCoreEventClass
			andEventID:kAEReopenApplication];
	});
}
