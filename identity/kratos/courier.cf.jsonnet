function(ctx) {
  to: ctx.recipient,
  channel: if std.objectHas(ctx, "channel") then ctx.channel else "email",
  subject: ctx.subject,
  html: ctx.body,
  template_type: ctx.template_type,
  data: if std.objectHas(ctx, "template_data") then ctx.template_data else {},
}
