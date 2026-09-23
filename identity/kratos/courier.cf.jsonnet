function(ctx) {
  to: ctx.recipient,
  subject: ctx.subject,
  html: ctx.body,
  template_type: ctx.template_type,
  data: if std.objectHas(ctx, "template_data") then ctx.template_data else {},
}
